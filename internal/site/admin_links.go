package site

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminLinkInput struct {
	ID            uint   `json:"id"`
	EffectiveDate string `json:"effective_date"`
	SourceFile    string `json:"source_file"`
	SiteKey       string `json:"site_key"`
	AppSlug       string `json:"app_slug"`
	Kind          string `json:"type"`
	Language      string `json:"lang"`
	Domain        string `json:"domain"`
	AnchorText    string `json:"anchor_text"`
	TargetURL     string `json:"target_url"`
	URL           string `json:"url"`
	Label         string `json:"label"`
	Title         string `json:"title"`
	Path          string `json:"path"`
	Category      string `json:"category"`
	Weight        *int   `json:"weight"`
	Enabled       *bool  `json:"enabled"`
	Remark        string `json:"remark"`
}

func adminCollection(path string) string {
	if strings.Contains(path, "internal-domains") {
		return "internal-domain"
	}
	if strings.Contains(path, "main-page-external-links") {
		return "main-external"
	}
	if strings.Contains(path, "external-links") {
		return "external"
	}
	return ""
}

func (a *adminServer) normalizeLink(b adminLinkInput, collection string) (SiteLink, InternalDomain, error) {
	fail := func(message string) (SiteLink, InternalDomain, error) {
		return SiteLink{}, InternalDomain{}, fmt.Errorf("%s", message)
	}
	if collection == "" {
		collection = b.Kind
	}
	if collection == "" {
		collection = "external"
	}
	if collection != "external" && collection != "internal" && collection != "main-external" && collection != "internal-domain" {
		return fail("invalid link type")
	}
	if b.SiteKey == "" {
		b.SiteKey = "default"
	}
	if err := a.runtime.validSite(b.SiteKey); err != nil {
		return fail(err.Error())
	}
	if b.Language == "" {
		b.Language = "id"
	}
	b.Language = strings.ToLower(strings.TrimSpace(b.Language))
	if !languagePattern.MatchString(b.Language) && !(b.Language == "global" && (collection == "external" || collection == "main-external")) {
		return fail("invalid link metadata language")
	}
	if b.AppSlug != "" {
		if !slugPattern.MatchString(b.AppSlug) {
			return fail("invalid app_slug")
		}
		if _, ok := a.runtime.Snapshot().FindApp(b.AppSlug); !ok {
			return fail("unknown app_slug")
		}
	}
	if (collection == "main-external" || collection == "internal-domain") && b.AppSlug != "" {
		return fail("collection does not support app_slug")
	}
	domain := strings.ToLower(strings.TrimSpace(b.Domain))
	if domain != "" && domain != "global" {
		var err error
		domain, err = normalizeAdminDomain(domain)
		if err != nil {
			return fail(err.Error())
		}
	}
	if b.TargetURL == "" {
		b.TargetURL = b.URL
	}
	if b.AnchorText == "" {
		b.AnchorText = b.Label
	}
	weight, enabled := 100, true
	if b.Weight != nil {
		weight = *b.Weight
	}
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	if weight < 0 || weight > 10000 {
		return fail("weight must be 0..10000")
	}
	if validateText(b.Category, 100) != nil || validateText(b.Remark, 500) != nil {
		return fail("invalid category or remark")
	}
	if collection == "internal-domain" {
		if domain == "" || domain == "global" {
			return fail("internal domain required")
		}
		if b.Path == "" {
			b.Path = "/"
		}
		if err := validateAdminPath(b.Path, false); err != nil {
			return fail(err.Error())
		}
		if b.Title == "" {
			b.Title = domain
		}
		if validateText(b.Title, 255) != nil {
			return fail("invalid title")
		}
		return SiteLink{}, InternalDomain{ID: b.ID, SiteKey: b.SiteKey, Domain: domain, Title: b.Title, Path: b.Path, Language: b.Language, Category: b.Category, Weight: weight, Enabled: enabled, Remark: b.Remark}, nil
	}
	if b.AnchorText == "" {
		b.AnchorText = b.TargetURL
	}
	if strings.TrimSpace(b.AnchorText) == "" || validateText(b.AnchorText, 255) != nil {
		return fail("invalid anchor_text")
	}
	if err := validateAdminURL(b.TargetURL, collection == "internal", false); err != nil {
		return fail(err.Error())
	}
	return SiteLink{ID: b.ID, SiteKey: b.SiteKey, AppSlug: b.AppSlug, Kind: collection, Language: b.Language, Domain: domain, AnchorText: b.AnchorText, TargetURL: b.TargetURL, Category: b.Category, Weight: weight, Enabled: enabled, Remark: b.Remark}, InternalDomain{}, nil
}

func adminLinkJSON(l SiteLink) gin.H {
	return gin.H{"id": l.ID, "site_key": l.SiteKey, "app_slug": l.AppSlug, "type": l.Kind, "lang": l.Language, "domain": l.Domain, "anchor_text": l.AnchorText, "target_url": l.TargetURL, "url": l.TargetURL, "label": l.AnchorText, "category": l.Category, "weight": l.Weight, "enabled": l.Enabled, "remark": l.Remark}
}

func (a *adminServer) links(c *gin.Context) {
	collection := adminCollection(c.FullPath())
	if c.Request.Method == "GET" {
		a.listLinks(c, collection)
		return
	}
	if c.Request.Method == "DELETE" {
		a.deleteLinks(c, collection)
		return
	}
	var b adminLinkInput
	if !adminDecode(c, &b) {
		return
	}
	if collection == "" {
		collection = b.Kind
		if collection == "" {
			collection = "external"
		}
		if collection != "internal" && collection != "external" {
			adminError(c, 422, "type must be internal or external")
			return
		}
	}
	if c.Request.Method == "PUT" && b.ID == 0 {
		adminError(c, 422, "id required for update")
		return
	}
	if b.EffectiveDate != "" {
		date := NormalizeHubDate(b.EffectiveDate)
		if date == "" {
			adminError(c, 422, "invalid effective_date")
			return
		}
		b.EffectiveDate = date
	}
	if validateText(b.SourceFile, 160) != nil {
		adminError(c, 422, "invalid source_file")
		return
	}
	l, d, err := a.normalizeLink(b, collection)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	if !a.commit(c, func(tx *gorm.DB) error {
		if collection == "internal-domain" {
			if d.ID == 0 {
				if err := tx.Create(&d).Error; err != nil {
					return err
				}
				return reindexRuntimeInternalDomains(tx, d.SiteKey)
			}
			res := tx.Model(&InternalDomain{}).Where("id = ? AND site_key = ?", d.ID, d.SiteKey).Select("Domain", "Title", "Path", "Language", "Category", "Weight", "Enabled", "Remark").Updates(&d)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
			return nil
		}
		if l.ID == 0 {
			if err := adminCreateLinkWithMetadata(tx, &l, b.EffectiveDate, b.SourceFile); err != nil {
				return err
			}
			if l.Kind == "external" && l.SiteKey == "default" {
				if err := appendRuntimeExternal(tx, []SiteLink{l}, b.EffectiveDate, b.SourceFile); err != nil {
					return err
				}
			}
			return reindexRuntimeLinks(tx, l.SiteKey, l.Kind, l.Kind == "main-external")
		}
		res := tx.Model(&SiteLink{}).Where("id = ? AND site_key = ? AND kind = ?", l.ID, l.SiteKey, l.Kind).Select("AppSlug", "Language", "Domain", "AnchorText", "TargetURL", "Category", "Weight", "Enabled", "Remark").Updates(&l)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		var m RuntimeLinkMetadata
		err := tx.First(&m, "link_id = ?", l.ID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := saveRuntimeLinkMetadata(tx, l, m.EffectiveDate, m.SourceFile, m.PHPID); err != nil {
			return err
		}
		if m.PHPID == 0 {
			return reindexRuntimeLinks(tx, l.SiteKey, l.Kind, l.Kind == "main-external")
		}
		return nil
	}) {
		return
	}
	if collection == "internal-domain" {
		adminOK(c, gin.H{"link": d})
	} else {
		adminOK(c, gin.H{"link": adminLinkJSON(l)})
	}
}

// Gorm's struct defaults would turn explicit false and zero into true and one.
func adminCreateLink(tx *gorm.DB, l *SiteLink) error {
	if err := adminCreateLinkWithMetadata(tx, l, "", ""); err != nil {
		return err
	}
	if l.Kind == "external" && l.SiteKey == "default" {
		if err := appendRuntimeExternal(tx, []SiteLink{*l}, "", ""); err != nil {
			return err
		}
	}
	return reindexRuntimeLinks(tx, l.SiteKey, l.Kind, l.Kind == "main-external")
}
func adminCreateLinkWithMetadata(tx *gorm.DB, l *SiteLink, date, source string) error {
	if date == "" && l.Kind == "external" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	enabled, weight := l.Enabled, l.Weight
	if err := tx.Create(l).Error; err != nil {
		return err
	}
	l.Enabled, l.Weight = enabled, weight
	if err := tx.Model(l).Updates(map[string]any{"enabled": enabled, "weight": weight}).Error; err != nil {
		return err
	}
	// The caller assigns logical IDs once after the complete mutation, before
	// committing the revision. Zero is never published for new admin rows.
	if err := saveRuntimeLinkMetadata(tx, *l, date, source, 0); err != nil {
		return err
	}
	return nil
}

func (a *adminServer) listLinks(c *gin.Context, collection string) {
	limit, offset, err := adminPagination(c)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	var filterEnabled *bool
	if raw := c.Query("enabled"); raw != "" {
		v, e := strconv.ParseBool(raw)
		if e != nil {
			adminError(c, 422, "invalid enabled filter")
			return
		}
		filterEnabled = &v
	}
	q := strings.ToLower(c.Query("q"))
	lang := c.Query("lang")
	domain := c.Query("domain")
	site := c.DefaultQuery("site_key", "default")
	match := func(key, l, d, text string, enabled bool) bool {
		return key == site && (filterEnabled == nil || enabled == *filterEnabled) && (lang == "" || lang == l) && (domain == "" || domain == d) && (q == "" || strings.Contains(strings.ToLower(text), q))
	}
	s := a.runtime.Snapshot()
	if collection == "internal-domain" {
		rows := []InternalDomain{}
		for _, d := range s.InternalDomains {
			if match(d.SiteKey, d.Language, d.Domain, d.Domain+" "+d.Title+" "+d.Path+" "+d.Remark, d.Enabled) {
				rows = append(rows, d)
			}
		}
		total := len(rows)
		start := min(offset, total)
		adminOK(c, gin.H{"links": rows[start:min(start+limit, total)], "internal_domains": rows[start:min(start+limit, total)], "total": total, "limit": limit, "offset": offset})
		return
	}
	rows := []gin.H{}
	internal, external := []gin.H{}, []gin.H{}
	for _, l := range s.Links {
		if collection != "" && l.Kind != collection {
			continue
		}
		if collection == "" && l.Kind != "internal" && l.Kind != "external" {
			continue
		}
		if !match(l.SiteKey, l.Language, l.Domain, l.AnchorText+" "+l.TargetURL+" "+l.Remark, l.Enabled) {
			continue
		}
		row := adminLinkJSON(l)
		if m, ok := s.LinkMetadata[l.ID]; ok {
			row["php_id"] = m.PHPID
			row["effective_date"] = m.EffectiveDate
			row["source_file"] = m.SourceFile
		}
		rows = append(rows, row)
		if l.Kind == "internal" {
			internal = append(internal, row)
		} else {
			external = append(external, row)
		}
	}
	if collection == "" {
		adminOK(c, gin.H{"internal": internal, "external": external})
		return
	}
	total := len(rows)
	start := min(offset, total)
	key := "external_links"
	if collection == "main-external" {
		key = "main_page_external_links"
	}
	adminOK(c, gin.H{"links": rows[start:min(start+limit, total)], key: rows[start:min(start+limit, total)], "total": total, "limit": limit, "offset": offset})
}

func (a *adminServer) deleteLinks(c *gin.Context, collection string) {
	var b adminDelete
	if !adminDecode(c, &b) {
		return
	}
	if b.ID != 0 {
		b.IDs = append(b.IDs, b.ID)
	}
	if !b.All && len(b.IDs) == 0 {
		adminError(c, 422, "ids or all required")
		return
	}
	for _, id := range b.IDs {
		if id == 0 {
			adminError(c, 422, "invalid id")
			return
		}
	}
	if b.SiteKey == "" {
		b.SiteKey = "default"
	}
	if err := a.runtime.validSite(b.SiteKey); err != nil {
		adminError(c, 422, err.Error())
		return
	}
	if collection == "" {
		collection = b.Kind
		if collection != "internal" && collection != "external" {
			adminError(c, 422, "type required")
			return
		}
	}
	var n int64
	if !a.commit(c, func(tx *gorm.DB) error {
		q := tx.Where("site_key = ?", b.SiteKey)
		if !b.All {
			q = q.Where("id IN ?", b.IDs)
		}
		var res *gorm.DB
		if collection == "internal-domain" {
			res = q.Delete(&InternalDomain{})
		} else {
			ids := tx.Model(&SiteLink{}).Select("id").Where("site_key = ? AND kind = ?", b.SiteKey, collection)
			if !b.All {
				ids = ids.Where("id IN ?", b.IDs)
			}
			if err := tx.Where("link_id IN (?)", ids).Delete(&RuntimeLinkMetadata{}).Error; err != nil {
				return err
			}
			res = q.Where("kind = ?", collection).Delete(&SiteLink{})
		}
		n = res.RowsAffected
		if res.Error != nil {
			return res.Error
		}
		if collection == "internal-domain" {
			return reindexRuntimeInternalDomains(tx, b.SiteKey)
		}
		if err := reindexRuntimeLinks(tx, b.SiteKey, collection, false); err != nil {
			return err
		}
		if collection == "external" && b.SiteKey == "default" {
			return rebuildRuntimeExternal(tx)
		}
		return nil
	}) {
		return
	}
	adminOK(c, gin.H{"deleted": n})
}

// Normalized records are an explicit opt-in, never inferred from legacy text.
// In this mode markup is field data, not an alternative source of link metadata.
func normalizedAdminImportRow(line string) ([]string, error) {
	var candidate []string
	for _, delimiter := range []rune{'|', '\t', ','} {
		r := csv.NewReader(strings.NewReader(line))
		r.Comma, r.TrimLeadingSpace, r.FieldsPerRecord = delimiter, true, -1
		fields, err := r.Read()
		if err != nil || len(fields) != 8 {
			continue
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		// A label can legally contain seven occurrences of another delimiter.
		// Field count alone therefore cannot identify the record's separator.
		// Select only a schema-valid candidate; full collection/runtime validation
		// still runs before the transaction. Never guess between valid schemas.
		if !normalizedAdminFieldsValid(fields) {
			continue
		}
		if candidate != nil {
			return nil, fmt.Errorf("ambiguous normalized delimiter; quote field data containing separators")
		}
		candidate = fields
	}
	if candidate == nil {
		return nil, fmt.Errorf("expected eight valid normalized columns (pipe, tab, or comma separated)")
	}
	return candidate, nil
}

func normalizedAdminFieldsValid(fields []string) bool {
	domain, language := strings.ToLower(fields[0]), strings.ToLower(fields[1])
	if domain != "" && domain != "global" {
		if _, err := normalizeAdminDomain(domain); err != nil {
			return false
		}
	}
	if language != "" && language != "global" && !languagePattern.MatchString(language) {
		return false
	}
	if validateAdminURL(fields[3], false, false) != nil || validateText(fields[2], 255) != nil || validateText(fields[4], 100) != nil || validateText(fields[7], 500) != nil {
		return false
	}
	if fields[5] != "" {
		weight, err := strconv.Atoi(fields[5])
		if err != nil || weight < 0 || weight > 10000 {
			return false
		}
	}
	if fields[6] != "" {
		if _, err := strconv.ParseBool(fields[6]); err != nil {
			return false
		}
	}
	return true
}

func splitAdminImport(raw string) ([][]string, error) {
	return splitAdminImportFormat(raw, "legacy")
}

func splitAdminImportFormat(raw, format string) ([][]string, error) {
	if format != "" && format != "legacy" && format != "normalized" {
		return nil, fmt.Errorf("format must be legacy or normalized")
	}
	var rows [][]string
	for n, line := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Admission is format-independent, including the HTML early-continue path.
		if len(rows) >= 10000 {
			return nil, fmt.Errorf("import is limited to 10000 rows")
		}
		if format == "normalized" {
			fields, err := normalizedAdminImportRow(line)
			if err != nil {
				return nil, fmt.Errorf("invalid import row %d: %w", n+1, err)
			}
			rows = append(rows, fields)
			continue
		}
		// Preserve PHP's anchor-first legacy behavior, including ambiguous
		// eight-column text. Only explicit normalized input bypasses this.
		if href, label := extractAdminAnchor(line); href != "" {
			rows = append(rows, []string{href, label})
			continue
		}
		// PHP preg_split('/[,|\t]/', row): mixed delimiters, empty fields
		// retained, and quotes have no escaping semantics in legacy mode.
		fields := strings.Split(strings.ReplaceAll(strings.ReplaceAll(line, "\t", ","), "|", ","), ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("import contains no rows")
	}
	return rows, nil
}

func adminLinkKey(l SiteLink) string {
	parts := []string{l.Domain, l.Language, l.AnchorText, l.TargetURL}
	if l.Kind == "internal" {
		parts = []string{l.TargetURL}
	}
	// PHP strtolower is ASCII-only. Site/app namespaces are Go extensions.
	return strings.Join([]string{l.SiteKey, l.AppSlug, l.Kind, legacyLower(strings.Join(parts, "|"))}, "\x00")
}
func adminDomainKey(d InternalDomain) string {
	return d.SiteKey + "\x00" + legacyLower(d.Domain+"|"+d.Path)
}

func (a *adminServer) importLinks(c *gin.Context) {
	var b struct {
		Type          string `json:"type"`
		Format        string `json:"format"`
		Links         string `json:"links"`
		Domains       string `json:"domains"`
		Strategy      string `json:"strategy"`
		SiteKey       string `json:"site_key"`
		Language      string `json:"lang"`
		Date          string `json:"date"`
		EffectiveDate string `json:"effective_date"`
		SourceFile    string `json:"source_file"`
	}
	if !adminDecode(c, &b) {
		return
	}
	collection := adminCollection(c.FullPath())
	if collection == "" {
		collection = b.Type
		if collection == "" {
			collection = "external"
		}
		if collection != "internal" && collection != "external" {
			adminError(c, 422, "type must be internal or external")
			return
		}
	}
	if b.Format == "normalized" && collection != "external" && collection != "main-external" {
		adminError(c, 422, "normalized format is only supported for external and main-external links")
		return
	}
	if b.EffectiveDate == "" {
		b.EffectiveDate = b.Date
	}
	if b.EffectiveDate != "" {
		date := NormalizeHubDate(b.EffectiveDate)
		if date == "" {
			adminError(c, 422, "invalid effective_date")
			return
		}
		b.EffectiveDate = date
	}
	if validateText(b.SourceFile, 160) != nil {
		adminError(c, 422, "invalid source_file")
		return
	}
	if b.Strategy == "" {
		b.Strategy = "replace"
		if collection == "main-external" {
			b.Strategy = "append"
		}
	}
	if b.Strategy != "replace" && b.Strategy != "append" {
		adminError(c, 422, "strategy must be replace or append")
		return
	}
	if b.SiteKey == "" {
		b.SiteKey = "default"
	}
	if err := a.runtime.validSite(b.SiteKey); err != nil {
		adminError(c, 422, err.Error())
		return
	}
	raw := b.Links
	if collection == "internal-domain" {
		raw = b.Domains
	}
	rows, err := splitAdminImportFormat(raw, b.Format)
	if err != nil {
		adminError(c, 422, err.Error())
		return
	}
	links := []SiteLink{}
	domains := []InternalDomain{}
	seen := map[string]bool{}
	duplicates := 0
	for n, row := range rows {
		in := adminLinkInput{SiteKey: b.SiteKey, Language: b.Language}
		// PHP import metadata defaults to en; the site's fallback bank stays id.
		if in.Language == "" && collection != "internal" {
			in.Language = "en"
		}
		get := func(i int) string {
			if i < len(row) {
				return row[i]
			}
			return ""
		}
		var weight, enabled string
		if collection == "internal-domain" {
			if len(row) > 8 {
				adminError(c, 422, fmt.Sprintf("row %d: expected at most 8 columns", n+1))
				return
			}
			in.Domain = get(0)
			in.Title = get(1)
			in.Path = get(2)
			if get(3) != "" {
				in.Language = get(3)
			}
			in.Category = get(4)
			weight = get(5)
			enabled = get(6)
			in.Remark = get(7)
		} else if collection != "internal" && len(row) == 8 {
			in.Domain = get(0)
			in.Language = get(1)
			in.AnchorText = get(2)
			in.TargetURL = get(3)
			in.Category = get(4)
			weight = get(5)
			enabled = get(6)
			in.Remark = get(7)
		} else {
			if len(row) > 2 {
				adminError(c, 422, fmt.Sprintf("row %d: expected URL,label or 8-column external row", n+1))
				return
			}
			in.TargetURL = get(0)
			in.AnchorText = get(1)
			if collection != "internal" {
				if u, e := url.Parse(in.TargetURL); e == nil {
					in.Domain = u.Hostname()
				}
			}
		}
		if weight != "" {
			v, e := strconv.Atoi(weight)
			if e != nil {
				adminError(c, 422, fmt.Sprintf("row %d: invalid weight", n+1))
				return
			}
			in.Weight = &v
		}
		if enabled != "" {
			v, e := strconv.ParseBool(enabled)
			if e != nil {
				adminError(c, 422, fmt.Sprintf("row %d: invalid enabled", n+1))
				return
			}
			in.Enabled = &v
		}
		l, d, e := a.normalizeLink(in, collection)
		if e != nil {
			adminError(c, 422, fmt.Sprintf("row %d: %s", n+1, e))
			return
		}
		key := adminLinkKey(l)
		if collection == "internal-domain" {
			key = adminDomainKey(d)
		}
		if seen[key] {
			duplicates++
			continue
		}
		seen[key] = true
		if collection == "internal-domain" {
			domains = append(domains, d)
		} else {
			links = append(links, l)
		}
	}
	inserted := 0
	if !a.commit(c, func(tx *gorm.DB) error {
		if collection == "internal-domain" {
			seen := map[string]bool{}
			if b.Strategy == "replace" {
				if err := tx.Where("site_key = ?", b.SiteKey).Delete(&InternalDomain{}).Error; err != nil {
					return err
				}
			} else {
				var old []InternalDomain
				if err := tx.Where("site_key = ?", b.SiteKey).Find(&old).Error; err != nil {
					return err
				}
				for _, d := range old {
					seen[adminDomainKey(d)] = true
				}
			}
			for i := range domains {
				if seen[adminDomainKey(domains[i])] {
					duplicates++
					continue
				}
				if err := tx.Create(&domains[i]).Error; err != nil {
					return err
				}
				inserted++
			}
			return reindexRuntimeInternalDomains(tx, b.SiteKey)
		}
		seen := map[string]bool{}
		q := tx.Where("site_key = ? AND kind = ?", b.SiteKey, collection)
		if b.Strategy == "replace" {
			if collection == "external" && b.SiteKey == "default" {
				if err := clearRuntimeExternal(tx); err != nil {
					return err
				}
			}
			if err := tx.Where("link_id IN (?)", tx.Model(&SiteLink{}).Select("id").Where("site_key = ? AND kind = ?", b.SiteKey, collection)).Delete(&RuntimeLinkMetadata{}).Error; err != nil {
				return err
			}
			if err := q.Delete(&SiteLink{}).Error; err != nil {
				return err
			}
		} else {
			var old []SiteLink
			if err := q.Find(&old).Error; err != nil {
				return err
			}
			for _, l := range old {
				seen[adminLinkKey(l)] = true
			}
		}
		for i := range links {
			if seen[adminLinkKey(links[i])] {
				duplicates++
				continue
			}
			if err := adminCreateLinkWithMetadata(tx, &links[i], b.EffectiveDate, b.SourceFile); err != nil {
				return err
			}
			inserted++
		}
		// PHP snapshots the parsed incoming batch BEFORE cross-batch ordinary
		// collection dedupe. A duplicate consumes a snapshot ID with its own
		// immutable payload (possibly changed weight/enabled), not a live join.
		if collection == "external" && b.SiteKey == "default" {
			if err := appendRuntimeExternal(tx, links, b.EffectiveDate, b.SourceFile); err != nil {
				return err
			}
		}
		return reindexRuntimeLinks(tx, b.SiteKey, collection, collection == "main-external" && b.Strategy == "append")
	}) {
		return
	}
	adminOK(c, gin.H{"imported": inserted, "duplicates": duplicates, "strategy": b.Strategy, "type": collection})
}
