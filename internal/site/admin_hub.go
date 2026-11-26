package site

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *adminServer) hubSettings(c *gin.Context) {
	if c.Request.Method == "GET" {
		adminOK(c, a.runtime.Snapshot().Hub.Config)
		return
	}
	var patch map[string]json.RawMessage
	if !adminDecode(c, &patch) {
		return
	}
	// Merge under the revision lock to avoid losing simultaneous partial edits.
	var cfg HubConfig

	if !a.commit(c, func(tx *gorm.DB) error {
		var state RuntimeHubState
		if err := tx.First(&state, 1).Error; err != nil {
			return err
		}
		old := DecodeHubConfig([]byte(state.ConfigJSON))
		encoded, _ := json.Marshal(old)
		var full map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &full)
		for key, value := range patch {
			if _, ok := full[key]; !ok {
				return fmt.Errorf("%w: unknown hub config field %s", errAdminValidation, key)
			}
			full[key] = value
		}
		encoded, _ = json.Marshal(full)
		if err := json.Unmarshal(encoded, &cfg); err != nil {
			return fmt.Errorf("%w: invalid field type", errAdminValidation)
		}
		if err := validateAdminHub(cfg); err != nil {
			return fmt.Errorf("%w: %v", errAdminValidation, err)
		}
		cfg = cfg.Normalize()
		encoded, _ = json.Marshal(cfg)
		if err := tx.Model(&state).Update("config_json", string(encoded)).Error; err != nil {
			return err
		}
		// PHP's panel settings also control detail pages; keep SiteConfig aligned.
		panelPatch := map[string]any{}
		for key, value := range map[string]bool{"show_list_panel": cfg.ShowListPanel, "show_subdomain_panel": cfg.ShowSubdomainPanel, "show_external_panel": cfg.ShowExternalPanel} {
			if _, ok := patch[key]; ok {
				panelPatch[key] = value
			}
		}
		if len(panelPatch) == 0 {
			return nil
		}
		return tx.Model(&SiteConfig{}).Where("site_key = ?", "default").Updates(panelPatch).Error
	}) {
		return
	}
	adminOK(c, cfg)
}

var errAdminValidation = fmt.Errorf("invalid admin data")

func validateAdminHub(cfg HubConfig) error {
	if !hubTriggerPattern.MatchString(cfg.TriggerKeyword) {
		return fmt.Errorf("invalid trigger_keyword")
	}
	if len(cfg.SubdomainDSL) > 300 || len(ParseHubDSL(cfg.SubdomainDSL)) == 0 {
		return fmt.Errorf("invalid subdomain_dsl")
	}
	size := 0
	for _, s := range ParseHubDSL(cfg.SubdomainDSL) {
		size += s.Length
	}
	if size > 63 {
		return fmt.Errorf("subdomain label exceeds 63 characters")
	}
	if cfg.SubdomainCount < 1 || cfg.SubdomainCount > 200 || cfg.ExternalMin < 1 || cfg.ExternalMax < cfg.ExternalMin || cfg.ExternalMax > 150000 || cfg.BottomLinkCount < 0 || cfg.BottomLinkCount > 50 || cfg.DateNavNumMax < 10 || cfg.DateNavNumMax > 9999 || cfg.IntroMinWords < 50 || cfg.IntroMinWords > 10000 {
		return fmt.Errorf("hub numeric option out of range")
	}
	if validateText(cfg.LinkRel, 120) != nil {
		return fmt.Errorf("invalid link_rel")
	}
	return nil
}

func (a *adminServer) hubDomains(c *gin.Context) {
	if c.Request.Method == "GET" {
		domain, err := normalizeAdminDomain(c.Query("domain"))
		if err != nil {
			adminError(c, 422, err.Error())
			return
		}
		prefixes := a.runtime.Snapshot().Hub.Subdomains[domain]
		if prefixes == nil {
			prefixes = []string{}
		}
		adminOK(c, gin.H{"domain": domain, "subdomains": prefixes})
		return
	}
	var b struct {
		Domain string `json:"domain"`
		DSL    string `json:"dsl"`
		Count  *int   `json:"count"`
	}
	if !adminDecode(c, &b) {
		return
	}
	domain, err := normalizeAdminDomain(b.Domain)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	cfg := a.runtime.Snapshot().Hub.Config
	if b.DSL == "" {
		b.DSL = cfg.SubdomainDSL
	}
	count := cfg.SubdomainCount
	if b.Count != nil {
		count = *b.Count
	}
	if count < 1 || count > 200 {
		adminError(c, 422, "count must be 1..200")
		return
	}
	size := 0
	for _, s := range ParseHubDSL(b.DSL) {
		size += s.Length
	}
	if len(b.DSL) > 300 || size > 63 || size+len(domain)+1 > 253 {
		adminError(c, 422, "subdomain DSL exceeds DNS length")
		return
	}
	prefixes, err := GenerateHubSubdomains(strings.TrimSpace(b.DSL), count)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	encoded, _ := json.Marshal(prefixes)
	if !a.commit(c, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "domain"}}, DoUpdates: clause.AssignmentColumns([]string{"prefixes", "updated_at"})}).Create(&RuntimeHubSubdomain{Domain: domain, Prefixes: string(encoded)}).Error
	}) {
		return
	}
	adminOK(c, gin.H{"domain": domain, "dsl": b.DSL, "count": len(prefixes), "subdomains": prefixes})
}
