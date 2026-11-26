package site

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// RegisterAdminRoutes adds only /admin routes to an existing public router.
func RegisterAdminRoutes(router *gin.Engine, runtime *Runtime, opts AdminOptions) error {
	if router == nil || runtime == nil || runtime.Snapshot() == nil {
		return fmt.Errorf("admin requires router and initialized runtime")
	}
	if opts.Origin != "" {
		u, err := parseAdminOrigin(opts.Origin)
		if err != nil {
			return err
		}
		opts.SecureCookies = opts.SecureCookies || u.Scheme == "https"
		opts.Origin = u.String()
	}
	if opts.CookieName == "" {
		opts.CookieName = "apk_admin_session"
	}
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = 8 * time.Hour
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-login-hash-unusable"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a := &adminServer{runtime: runtime, opts: opts, dummyHash: dummy}
	page, err := template.ParseFS(templateFiles, "templates/admin.html")
	if err != nil {
		return err
	}
	group := router.Group("/admin")
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		c.Next()
	})
	for _, p := range []string{"", "/", "/login"} {
		group.GET(p, func(c *gin.Context) {
			c.Header("Content-Type", "text/html; charset=utf-8")
			if err := page.ExecuteTemplate(c.Writer, "admin.html", nil); err != nil {
				c.AbortWithStatus(500)
			}
		})
	}
	group.GET("/api/session", a.sessionInfo)
	for _, p := range []string{"/api/session/login", "/api/session/verify"} {
		group.POST(p, a.require(false), a.login)
	}
	group.POST("/api/session/logout", a.require(true), a.logout)
	protected := group.Group("")
	protected.Use(a.require(true))
	protected.GET("/api/sites/config", a.settings)
	protected.POST("/api/sites/config", a.settings)
	for _, p := range []string{"/api/domain-lang-bindings", "/api/domain-image-bindings"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			protected.Handle(method, p, a.bindings)
		}
	}
	for _, p := range []string{"/api/sites/links", "/api/external-links", "/api/main-page-external-links", "/api/internal-domains"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			protected.Handle(method, p, a.links)
		}
	}
	for _, p := range []string{"/api/sites/links/import", "/api/main-page-external-links/import", "/api/internal-domains/import"} {
		protected.POST(p, a.importLinks)
	}
	protected.GET("/api/hub/config", a.hubSettings)
	protected.POST("/api/hub/config", a.hubSettings)
	protected.GET("/api/hub/subdomains", a.hubDomains)
	protected.POST("/api/hub/subdomains/refresh", a.hubDomains)
	protected.POST("/api/external-links/preprocess", a.preprocess)
	protected.POST("/api/lan/persist/jobs", a.enqueue)
	protected.GET("/api/lan/persist/jobs", a.jobs)
	protected.GET("/api/lan/persist/status", a.jobs)
	protected.POST("/api/lan/persist/jobs/:id/retry", a.retry)
	protected.GET("/lan/status", a.jobs)
	protected.POST("/lan/refresh", a.enqueue)
	return nil
}

func (a *adminServer) commit(c *gin.Context, change func(*gorm.DB) error) bool {
	if err := UpdateRuntime(c.Request.Context(), a.runtime.db, change); err != nil {
		if errors.Is(err, errAdminValidation) {
			adminError(c, 422, err.Error())
		} else if errors.Is(err, errAdminDuplicate) {
			adminError(c, 409, "domain binding already exists; use PUT to update")
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			adminError(c, 404, "record not found")
		} else {
			adminError(c, 503, "database write failed")
		}
		return false
	}
	if err := a.runtime.Reload(c.Request.Context()); err != nil {
		// The write is committed; do not claim failure and invite a duplicate retry.
		c.Header("X-Runtime-Reload", "pending")
		if a.runtime.opts.OnError != nil {
			a.runtime.opts.OnError(err)
		}
	}
	return true
}

func adminConfig(cfg SiteConfig) gin.H {
	return gin.H{"site_key": cfg.SiteKey, "site_name": cfg.DisplayName, "default_lang": cfg.DefaultLanguage, "route_prefix": cfg.RoutePrefix, "download_base_url": cfg.DownloadBaseURL, "image_base_url": cfg.ImageBasePath, "main_page_external_link_limit": cfg.ExternalLinkLimit, "show_list_panel": cfg.ShowListPanel, "show_subdomain_panel": cfg.ShowSubdomainPanel, "show_external_panel": cfg.ShowExternalPanel, "max_related_links": cfg.MaxRelatedLinks}
}

func (a *adminServer) settings(c *gin.Context) {
	if c.Request.Method == "GET" {
		s := a.runtime.Snapshot()
		key := c.DefaultQuery("site_key", "default")
		if err := a.runtime.validSite(key); err != nil {
			adminError(c, 422, err.Error())
			return
		}
		cfg := s.Config
		for _, v := range s.Configs {
			if v.SiteKey == key {
				cfg = v
				break
			}
		}
		adminOK(c, gin.H{"site_config": adminConfig(cfg), "supported_langs": s.Languages, "revision": s.Revision})
		return
	}
	var b struct {
		SiteKey   string  `json:"site_key"`
		Name      *string `json:"site_name"`
		Language  *string `json:"default_lang"`
		Prefix    *string `json:"route_prefix"`
		Download  *string `json:"download_base_url"`
		Image     *string `json:"image_base_url"`
		Limit     *int    `json:"main_page_external_link_limit"`
		List      *bool   `json:"show_list_panel"`
		Subdomain *bool   `json:"show_subdomain_panel"`
		External  *bool   `json:"show_external_panel"`
		Related   *int    `json:"max_related_links"`
	}
	if !adminDecode(c, &b) {
		return
	}
	if b.SiteKey == "" {
		b.SiteKey = "default"
	}
	if err := a.runtime.validSite(b.SiteKey); err != nil {
		adminError(c, 422, err.Error())
		return
	}
	patch := map[string]any{}
	if b.Name != nil {
		if strings.TrimSpace(*b.Name) == "" || validateText(*b.Name, 200) != nil {
			adminError(c, 422, "invalid site_name")
			return
		}
		patch["display_name"] = *b.Name
	}
	if b.Language != nil {
		*b.Language = strings.ToLower(strings.TrimSpace(*b.Language))
		if err := a.runtime.validLanguage(*b.Language); err != nil {
			adminError(c, 422, err.Error())
			return
		}
		patch["default_language"] = *b.Language
	}
	if b.Prefix != nil {
		if len(*b.Prefix) > 120 || validateText(*b.Prefix, 120) != nil || strings.ContainsAny(*b.Prefix, "\t") {
			adminError(c, 422, "invalid route_prefix")
			return
		}
		patch["route_prefix"] = NormalizeLegacyPrefix(*b.Prefix)
	}
	if b.Download != nil {
		if len(*b.Download) > 500 || validateAdminURL(*b.Download, true, true) != nil {
			adminError(c, 422, "invalid download_base_url")
			return
		}
		patch["download_base_url"] = *b.Download
	}
	if b.Image != nil {
		if a.runtime.validateImageBase(*b.Image) != nil {
			adminError(c, 422, "invalid image_base_url")
			return
		}
		patch["image_base_path"] = *b.Image
	}
	if b.Limit != nil {
		if *b.Limit < 0 || *b.Limit > 200 {
			adminError(c, 422, "link limit must be 0..200")
			return
		}
		patch["external_link_limit"] = *b.Limit
	}
	if b.Related != nil {
		if *b.Related < 0 || *b.Related > legacyMaxRelatedLinks {
			adminError(c, 422, "max_related_links must be 0..6")
			return
		}
		patch["max_related_links"] = *b.Related
	}
	if b.List != nil {
		patch["show_list_panel"] = *b.List
	}
	if b.Subdomain != nil {
		patch["show_subdomain_panel"] = *b.Subdomain
	}
	if b.External != nil {
		patch["show_external_panel"] = *b.External
	}
	var cfg SiteConfig
	if !a.commit(c, func(tx *gorm.DB) error {
		err := tx.Where("site_key = ?", b.SiteKey).First(&cfg).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cfg = a.runtime.Snapshot().Config
			cfg.ID = 0
			cfg.SiteKey = b.SiteKey
			if e := tx.Create(&cfg).Error; e != nil {
				return e
			}
		} else if err != nil {
			return err
		}
		if err := tx.Model(&cfg).Updates(patch).Error; err != nil {
			return err
		}
		if b.SiteKey == "default" && (b.List != nil || b.Subdomain != nil || b.External != nil) {
			var state RuntimeHubState
			if err := tx.First(&state, 1).Error; err != nil {
				return err
			}
			hub := DecodeHubConfig([]byte(state.ConfigJSON))
			if b.List != nil {
				hub.ShowListPanel = *b.List
			}
			if b.Subdomain != nil {
				hub.ShowSubdomainPanel = *b.Subdomain
			}
			if b.External != nil {
				hub.ShowExternalPanel = *b.External
			}
			raw, _ := json.Marshal(hub)
			if err := tx.Model(&state).Update("config_json", string(raw)).Error; err != nil {
				return err
			}
		}
		return tx.First(&cfg, cfg.ID).Error
	}) {
		return
	}
	adminOK(c, adminConfig(cfg))
}

var errAdminDuplicate = errors.New("duplicate domain binding")

type adminDelete struct {
	ID      uint     `json:"id"`
	IDs     []uint   `json:"ids"`
	Domain  string   `json:"domain"`
	Domains []string `json:"domains"`
	All     bool     `json:"all"`
	SiteKey string   `json:"site_key"`
	Kind    string   `json:"type"`
}

func (a *adminServer) bindings(c *gin.Context) {
	image := strings.Contains(c.FullPath(), "image")
	list := func() []gin.H {
		rows := []gin.H{}
		s := a.runtime.Snapshot()
		if image {
			for _, b := range s.ImageBindings {
				rows = append(rows, gin.H{"id": b.ID, "domain": b.Domain, "site_key": b.SiteKey, "image_base_url": b.ImageBasePath, "remark": b.Remark})
			}
		} else {
			for _, b := range s.DomainBindings {
				rows = append(rows, gin.H{"id": b.ID, "domain": b.Domain, "site_key": b.SiteKey, "lang": b.Language, "remark": b.Remark})
			}
		}
		return rows
	}
	if c.Request.Method == "GET" {
		adminOK(c, gin.H{"bindings": list(), "supported_langs": a.runtime.opts.Languages})
		return
	}
	if c.Request.Method == "DELETE" {
		var b adminDelete
		if !adminDecode(c, &b) {
			return
		}
		if b.Domain != "" {
			b.Domains = append(b.Domains, b.Domain)
		}
		if !b.All && len(b.Domains) == 0 {
			adminError(c, 422, "domains or all required")
			return
		}
		for i, d := range b.Domains {
			v, err := normalizeAdminDomain(d)
			if err != nil {
				adminError(c, 422, err.Error())
				return
			}
			b.Domains[i] = v
		}
		var n int64
		if !a.commit(c, func(tx *gorm.DB) error {
			q := tx.Where("1 = 1")
			if !b.All {
				q = q.Where("domain IN ?", b.Domains)
			}
			var res *gorm.DB
			if image {
				res = q.Delete(&DomainImageBinding{})
			} else {
				res = q.Delete(&DomainBinding{})
			}
			n = res.RowsAffected
			return res.Error
		}) {
			return
		}
		adminOK(c, gin.H{"deleted": n, "bindings": list()})
		return
	}
	var b struct {
		Domain   string `json:"domain"`
		Language string `json:"lang"`
		Image    string `json:"image_base_url"`
		SiteKey  string `json:"site_key"`
		Remark   string `json:"remark"`
	}
	if !adminDecode(c, &b) {
		return
	}
	d, err := normalizeAdminDomain(b.Domain)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	if b.SiteKey == "" {
		b.SiteKey = "default"
	}
	if err = a.runtime.validSite(b.SiteKey); err != nil {
		adminError(c, 422, err.Error())
		return
	}
	if validateText(b.Remark, 500) != nil {
		adminError(c, 422, "invalid remark")
		return
	}
	if image {
		if a.runtime.validateImageBase(b.Image) != nil {
			adminError(c, 422, "invalid image_base_url")
			return
		}
	} else {
		b.Language = strings.ToLower(strings.TrimSpace(b.Language))
		if err = a.runtime.validLanguage(b.Language); err != nil {
			adminError(c, 422, err.Error())
			return
		}
	}
	if !a.commit(c, func(tx *gorm.DB) error {
		var model any = &DomainBinding{}
		if image {
			model = &DomainImageBinding{}
		}
		var count int64
		if err := tx.Model(model).Where("domain = ?", d).Count(&count).Error; err != nil {
			return err
		}
		if c.Request.Method == "POST" {
			if count != 0 {
				return errAdminDuplicate
			}
			if image {
				return tx.Create(&DomainImageBinding{Domain: d, SiteKey: b.SiteKey, ImageBasePath: b.Image, Remark: b.Remark}).Error
			}
			return tx.Create(&DomainBinding{Domain: d, SiteKey: b.SiteKey, Language: b.Language, Remark: b.Remark}).Error
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
		patch := map[string]any{"site_key": b.SiteKey, "remark": b.Remark}
		if image {
			patch["image_base_path"] = b.Image
		} else {
			patch["language"] = b.Language
		}
		return tx.Model(model).Where("domain = ?", d).Updates(patch).Error
	}) {
		return
	}
	adminOK(c, gin.H{"bindings": list()})
}

func adminPagination(c *gin.Context) (int, int, error) {
	limit, offset := 50, 0
	var err error
	if v := c.Query("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid limit")
		}
	}
	if v := c.Query("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid offset")
		}
	}
	if limit < 1 || limit > 500 || offset < 0 {
		return 0, 0, fmt.Errorf("limit must be 1..500 and offset nonnegative")
	}
	return limit, offset, nil
}
