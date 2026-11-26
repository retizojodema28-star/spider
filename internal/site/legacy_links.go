package site

import (
	"bytes"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	legacyMaxRelatedLinks      = 6
	legacyMaxPageExternalLinks = 200
)

var legacyLinkWord = regexp.MustCompile(`(?i)(^|[^a-z0-9])link([^a-z0-9]|$)`)
var legacyDomainWord = regexp.MustCompile(`(?i)^[a-z0-9.-]+\.[a-z]{2,}$`)

// LegacySnapshotFromRuntime selects the formal default site's collections.
// Call LegacySnapshotFromRuntimeForSite when ConfigForHost selects another site.
func LegacySnapshotFromRuntime(s *Snapshot) LegacySnapshot {
	return LegacySnapshotFromRuntimeForSite(s, "default")
}

// LegacySnapshotFromRuntimeForSite selects a site's managed collections, not
// individual links by request domain, app slug, or language. It intentionally
// does not use LinksFor: PHP only applies language preference to main-page links.
// Blank selector means default; records must have the selected SiteKey exactly.
func LegacySnapshotFromRuntimeForSite(s *Snapshot, siteKey string) LegacySnapshot {
	if s == nil {
		return LegacySnapshot{}
	}
	if siteKey == "" {
		siteKey = "default"
	}
	out := LegacySnapshot{LanguageBank: s.Bank, Apps: s.Apps, DomainBindings: s.DomainBindings, ImageBindings: s.ImageBindings, LocalImageFiles: s.LocalImageFiles, HubEnabled: true, ExternalLinks: []LegacyManagedLink{}, MainExternalLinks: []LegacyManagedLink{}}
	for _, l := range s.Links {
		if l.SiteKey != siteKey {
			continue
		}
		out.Links = append(out.Links, l)
		row := legacyManagedFromSite(l)
		if m, ok := s.LinkMetadata[l.ID]; ok {
			// SQL IDs remain admin handles; PHP sorts/hashes collection-local IDs.
			// Zero only occurs in pre-migration snapshots or older test fixtures.
			if m.PHPID != 0 {
				row.ID = m.PHPID
			}
			row.PHPJSON = json.RawMessage(m.PHPJSON)
		}
		switch l.Kind {
		case "external":
			out.ExternalLinks = append(out.ExternalLinks, row)
		case "main_page_external", "main-external":
			out.MainExternalLinks = append(out.MainExternalLinks, row)
		}
	}
	for _, d := range s.InternalDomains {
		if d.SiteKey != siteKey {
			continue
		}
		id := d.PHPID
		if id == 0 {
			id = d.ID // compatibility for pre-migration snapshots/old fixtures
		}
		out.InternalDomains = append(out.InternalDomains, LegacyManagedLink{ID: id, Domain: d.Domain, Title: d.Title, Path: d.Path, Language: d.Language, Category: d.Category, Weight: d.Weight, Enabled: d.Enabled, Remark: d.Remark})
	}
	return out
}

func legacyManagedFromSite(l SiteLink) LegacyManagedLink {
	return LegacyManagedLink{ID: l.ID, Domain: l.Domain, Title: l.AnchorText, Path: l.TargetURL, Language: l.Language, AnchorText: l.AnchorText, TargetURL: l.TargetURL, Category: l.Category, Weight: l.Weight, Enabled: l.Enabled, Remark: l.Remark}
}

func legacyBuildLinks(p *LegacyPage, s LegacySnapshot) {
	maxRelated := legacyMaxRelatedLinks
	if s.MaxRelatedLinks != nil {
		maxRelated = min(max(0, *s.MaxRelatedLinks), legacyMaxRelatedLinks)
	}
	words := p.Keywords
	if len(words) == 0 {
		words = s.AppNames
	}
	candidates := []string{}
	for _, word := range legacyUnique(words, false) {
		if !legacyLinkWord.MatchString(word) {
			candidates = append(candidates, word)
		}
	}
	if maxRelated > 0 {
		pinned := min(2, min(maxRelated, len(candidates)))
		chosen := append([]string(nil), candidates[:pinned]...)
		remaining := legacyShuffle(legacyUnique(candidates[pinned:], true), func(word string) string { return sha1Hex(p.Hash + "|recommendation|item|" + word) })
		chosen = append(chosen, remaining[:min(maxRelated-pinned, len(remaining))]...)
		for i, word := range chosen {
			source, pin := "word_bank_rotation", 0
			if i < 2 {
				source, pin = "word_bank_primary", 1
			}
			p.RelatedLinks = append(p.RelatedLinks, LegacyLink{Title: word + " APK", Path: ApplyLegacyPrefix("/apps/"+legacySlug(word), p.RoutePrefix), Source: source, Pinned: pin})
		}
	}
	labels := legacyLabelsFor(p.Language)
	keyword := legacyNavWord(p.Brand)
	if keyword == "" {
		keyword = legacyNavWord(p.Host)
		suffixes := []string{}
		for _, lang := range []string{p.Language, "en", "id", "th", "vn"} {
			localized := legacyLabelsFor(lang).SEO.PathSuffixes
			for _, key := range []string{"official", "apk", "login", "download", "location", "discussion"} {
				suffixes = append(suffixes, localized[key])
			}
		}
		suffixes = legacyUnique(suffixes, false)
		sort.SliceStable(suffixes, func(i, j int) bool { return len(suffixes[i]) > len(suffixes[j]) })
		for _, suffix := range suffixes {
			if strings.HasSuffix(keyword, suffix) {
				keyword = strings.TrimSuffix(keyword, suffix)
				break
			}
		}
	}
	if keyword == "" {
		keyword = "apk"
	}
	for _, key := range []string{"official", "apk", "login", "download", "location", "discussion"} {
		suffix := labels.SEO.PathSuffixes[key]
		if p.Language == "th" && suffix != "apk" && !strings.HasPrefix(suffix, "apk") {
			suffix = "apk" + suffix
		}
		p.Navigation = append(p.Navigation, LegacyLink{Title: labels.Nav[key], Path: p.internalURL("/"+keyword+suffix+"/", true)})
	}
	p.ShowListPanel, p.ShowSubdomainPanel, p.ShowExternalPanel = s.ShowListPanel, s.ShowSubdomainPanel, s.ShowExternalPanel
	internal, external, main := append([]LegacyManagedLink(nil), s.InternalDomains...), append([]LegacyManagedLink(nil), s.ExternalLinks...), append([]LegacyManagedLink(nil), s.MainExternalLinks...)
	for _, link := range s.Links {
		row := legacyManagedFromSite(link)
		switch link.Kind {
		case "internal_domain", "internal-domain":
			if s.InternalDomains == nil {
				internal = append(internal, row)
			}
		case "external":
			if s.ExternalLinks == nil {
				external = append(external, row)
			}
		case "main_page_external", "main-external":
			if s.MainExternalLinks == nil {
				main = append(main, row)
			}
		}
	}
	if s.ShowListPanel {
		categories := s.Categories
		if categories == nil {
			categories = legacyCategories
		}
		list := []LegacyLink{}
		for _, cat := range categories {
			title := legacyCategory(cat, p.Language)
			switch p.Language {
			case "id":
				title = "Daftar APK " + title
			case "th":
				title = "รายการ APK " + title
			case "vn":
				title = "Danh sach APK " + legacyLower(title)
			default:
				title += " APK List"
			}
			if _, known := legacyCategoryText[legacyLower(legacyTrim(cat))]; !known {
				title = cat
				if len(title) > 0 && title[0] >= 'a' && title[0] <= 'z' {
					title = strings.ToUpper(title[:1]) + title[1:]
				}
				title += " APK List"
			}
			list = append(list, LegacyLink{Title: title, Path: "/category/" + slugifyPHP(cat)})
		}
		list = legacyShuffle(list, func(l LegacyLink) string { return sha1Hex(p.Hash + "|list|" + string(legacyJSON(l))) })
		p.ListLinks = list[:min(8, len(list))]
		for i := range p.ListLinks {
			p.ListLinks[i].Path = ApplyLegacyPrefix(p.ListLinks[i].Path, p.RoutePrefix)
		}
	}
	if s.ShowSubdomainPanel && maxRelated > 0 {
		rows := []LegacyManagedLink{}
		for _, row := range internal {
			if row.Enabled && row.Domain != "" {
				rows = append(rows, row)
			}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Weight != rows[j].Weight {
				return rows[i].Weight > rows[j].Weight
			}
			return rows[i].Domain < rows[j].Domain
		})
		rows = legacyShuffle(rows, func(l LegacyManagedLink) string {
			return sha1Hex(p.Hash + "|internal-domain|" + string(l.hashJSON(true)))
		})
		for _, row := range rows[:min(len(rows), max(1, maxRelated))] {
			path := row.Path
			if path == "" {
				path = "/"
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if strings.HasPrefix(path, "//") {
				path = "/"
			}
			p.SubdomainLinks = append(p.SubdomainLinks, LegacyLink{Title: row.Title, Path: "//" + row.Domain + path})
		}
		if len(p.SubdomainLinks) == 0 {
			root := legacyRootDomain(p.Host, s.Hosts)
			names := map[string][]string{"id": {"aplikasi", "unduhan", "aman", "pembaruan", "ulasan"}, "en": {"Apps", "Download", "Safe", "Update", "Review"}, "th": {"แอป", "ดาวน์โหลด", "ปลอดภัย", "อัปเดต", "รีวิว"}, "vn": {"ung dung", "tai ve", "an toan", "cap nhat", "danh gia"}}
			texts := names[p.Language]
			if texts == nil {
				texts = names["en"]
			}
			for i, sub := range []string{"apps", "download", "safe", "update", "review"} {
				title := texts[i] + " mirror"
				switch p.Language {
				case "id":
					title = "Mirror " + texts[i]
				case "th":
					title = "มิเรอร์" + texts[i]
				case "vn":
					title = "Ban sao " + texts[i]
				}
				p.SubdomainLinks = append(p.SubdomainLinks, LegacyLink{Title: title, Path: "//" + sub + "." + root + "/apps"})
			}
			p.SubdomainLinks = legacyShuffle(p.SubdomainLinks, func(l LegacyLink) string { return sha1Hex(p.Hash + "|subdomain|" + string(legacyJSON(l))) })
		}
	}
	externalLimit := max(0, min(legacyMaxPageExternalLinks, p.Config.ExternalLinkLimit))
	if s.ShowExternalPanel && externalLimit > 0 {
		rows := legacyExternalRows(external, legacyMaxPageExternalLinks)
		rows = legacyShuffle(rows, func(l LegacyManagedLink) string {
			return sha1Hex(p.Hash + "|external-link|" + string(l.hashJSON(false)))
		})
		rows = rows[:min(externalLimit, len(rows))]
		p.ExternalLinks = legacyExternalTargets(rows)
		if len(p.ExternalLinks) == 0 {
			base := p.Config.DownloadBaseURL
			if base == "" {
				base = "https://download.invalid/apk/"
			}
			for _, word := range words[:min(externalLimit, len(words))] {
				p.ExternalLinks = append(p.ExternalLinks, LegacyLink{Title: word + " Official APK", Path: strings.TrimRight(base, "/") + "/" + legacySlug(word) + ".apk", External: true})
			}
		}
	}
	if externalLimit > 0 {
		rows := legacyExternalRows(main, legacyMaxPageExternalLinks)
		preferred := []LegacyManagedLink{}
		for _, row := range rows {
			lang := legacyLower(row.Language)
			if lang == p.Language || lang == "" || lang == "global" {
				preferred = append(preferred, row)
			}
		}
		if len(preferred) > 0 {
			rows = preferred
		}
		rows = legacyShuffle(rows, func(l LegacyManagedLink) string {
			return sha1Hex(p.Hash + "|main-page-external-link|" + string(l.hashJSON(false)))
		})
		p.MainExternalLinks = legacyExternalTargets(rows[:min(externalLimit, len(rows))])
	}
	for _, item := range [][2]string{{"telegram", "TG"}, {"instagram", "IG"}, {"x", "X"}, {"youtube", "YT"}, {"link", "LK"}} {
		p.SocialLinks = append(p.SocialLinks, LegacyLink{Title: item[1], Path: p.internalURL("/"+item[0]+"/", false)})
	}
	for _, group := range []struct {
		key   string
		items []string
	}{{"footer_service", []string{"apk-install", "signature-verification", "download-service", "premium"}}, {"footer_developers", []string{"developer-console", "submit-apk", "traffic-monetization"}}, {"footer_company", []string{"about-us", "contact-us", "business-cooperation", "support-center"}}} {
		g := LegacyLinkGroup{Heading: p.Labels[group.key]}
		for _, item := range group.items {
			key := strings.ReplaceAll(item, "-", "_")
			if item == "premium" {
				key = "premium_service"
			}
			g.Links = append(g.Links, LegacyLink{Title: p.Labels[key], Path: p.internalURL("/"+item+"/", false)})
		}
		p.FooterGroups = append(p.FooterGroups, g)
	}
	for _, item := range []string{"privacy-policy", "copyright-policy", "terms", "eu-amau"} {
		p.LegalLinks = append(p.LegalLinks, LegacyLink{Title: p.Labels[strings.ReplaceAll(item, "-", "_")], Path: p.internalURL("/"+item+"/", false)})
	}
}

func legacyNavWord(s string) string {
	if parsed, err := url.Parse(s); err == nil && parsed.Hostname() != "" {
		return strings.Split(legacyLower(parsed.Hostname()), ".")[0]
	}
	if legacyDomainWord.MatchString(s) {
		return strings.Split(legacyLower(s), ".")[0]
	}
	return legacyRouteWord(legacyTrim(s))
}

func legacyRootDomain(host string, hosts []string) string {
	if host == "localhost" || net.ParseIP(host) != nil {
		for _, h := range hosts {
			if h != "localhost" && net.ParseIP(h) == nil && !strings.Contains(h, "*") {
				return h
			}
		}
		return "example.com"
	}
	parts := strings.Split(host, ".")
	return strings.Join(parts[max(0, len(parts)-2):], ".")
}

func legacyExternalRows(rows []LegacyManagedLink, limit int) []LegacyManagedLink {
	if limit <= 0 {
		return nil
	}
	out := []LegacyManagedLink{}
	for _, row := range rows {
		if row.Enabled && row.TargetURL != "" {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].ID > out[j].ID
	})
	return out[:min(len(out), min(legacyMaxPageExternalLinks, limit))]
}

func legacyExternalTargets(rows []LegacyManagedLink) []LegacyLink {
	out := []LegacyLink{}
	for _, row := range rows {
		if row.TargetURL != "" {
			out = append(out, LegacyLink{Title: row.AnchorText, Path: row.TargetURL, External: true})
		}
	}
	return out
}

func (l LegacyManagedLink) hashJSON(internal bool) []byte {
	if len(l.PHPJSON) > 0 {
		var b bytes.Buffer
		if json.Compact(&b, l.PHPJSON) == nil {
			return b.Bytes()
		}
	}
	if internal {
		return legacyJSON(struct {
			ID       uint   `json:"id"`
			Domain   string `json:"domain"`
			Title    string `json:"title"`
			Path     string `json:"path"`
			Language string `json:"lang"`
			Category string `json:"category"`
			Weight   int    `json:"weight"`
			Enabled  bool   `json:"enabled"`
			Remark   string `json:"remark"`
		}{l.ID, l.Domain, l.Title, l.Path, l.Language, l.Category, l.Weight, l.Enabled, l.Remark})
	}
	// Same ordered fields as PHP's normalized admin import. Imported rows with
	// additional/source fields must supply PHPJSON, since they affect the hash.
	return legacyJSON(struct {
		Domain   string `json:"domain"`
		Language string `json:"lang"`
		Anchor   string `json:"anchor_text"`
		Target   string `json:"target_url"`
		Category string `json:"category"`
		Weight   int    `json:"weight"`
		Enabled  bool   `json:"enabled"`
		Remark   string `json:"remark"`
		URL      string `json:"url"`
		Label    string `json:"label"`
		Source   string `json:"source_file"`
		ID       uint   `json:"id"`
	}{l.Domain, l.Language, l.AnchorText, l.TargetURL, l.Category, l.Weight, l.Enabled, l.Remark, l.TargetURL, l.AnchorText, "admin_stats_textarea", l.ID})
}
