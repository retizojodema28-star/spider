package site

import (
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HubSnapshot is immutable render input loaded by the MySQL snapshot worker.
// SnapshotMaxIDs retains PHP's cumulative date boundaries, including ID holes.
type HubSnapshot struct {
	Config              *HubConfig
	Subdomains          map[string][]string
	SnapshotMaxIDs      map[string]int
	ExternalRecords     map[int]HubExternalRecord
	ActiveDate          string
	MissingDateStrategy string // "nearest", otherwise PHP's "latest"
	ImageBaseURL        string // deprecated: ignored; images use indexed local site assets
	TierCounts          map[string]map[int]int
}

type HubExternalRecord struct {
	ID            int    `json:"id"`
	Domain        string `json:"domain"`
	AnchorText    string `json:"anchor_text"`
	TargetURL     string `json:"target_url"`
	RecommendTier int    `json:"recommend_tier"`
	Weight        int    `json:"weight"`
	Enabled       bool   `json:"enabled"`
}

type HubLink struct {
	Anchor        string `json:"anchor"`
	URL           string `json:"url"`
	Domain        string `json:"domain,omitempty"`
	RecommendTier int    `json:"recommend_tier,omitempty"`
}

func (l HubLink) Href() template.HTMLAttr { return legacyHref(l.URL) }

type HubSubdomain struct {
	Prefix string `json:"prefix"`
	Host   string `json:"host"`
	URL    string `json:"url"`
	Anchor string `json:"anchor"`
}

func (l HubSubdomain) Href() template.HTMLAttr { return legacyHref(l.URL) }

type HubDateLink struct {
	URL    string `json:"url"`
	Anchor string `json:"anchor"`
	Num    int    `json:"num"`
}

func (l HubDateLink) Href() template.HTMLAttr { return legacyHref(l.URL) }

type HubDateGroup struct {
	Date  string        `json:"date"`
	Label string        `json:"label"`
	Links []HubDateLink `json:"links"`
}

func hubHex(hash string, offset, length int) uint64 {
	if offset >= len(hash) {
		return 0
	}
	raw := hash[offset:min(len(hash), offset+length)]
	// PHP hexdec ignores non-hex characters rather than failing the conversion.
	var b strings.Builder
	for _, c := range raw {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			b.WriteRune(c)
		}
	}
	n, _ := strconv.ParseUint(b.String(), 16, 64)
	return n
}

func HubSubdomainNavigation(domain string, count int, hash string, prefixes []string) []HubSubdomain {
	out := []HubSubdomain{}
	if len(prefixes) == 0 || domain == "" {
		return out
	}
	offset := int(hubHex(hash, 16, 8) % uint64(len(prefixes)))
	for i := 0; i < min(count, len(prefixes)); i++ {
		prefix := prefixes[(offset+i)%len(prefixes)]
		host := prefix + "." + domain
		anchor := strings.ToUpper(prefix[:min(2, len(prefix))]) + fmt.Sprintf("-%02d", i+1)
		out = append(out, HubSubdomain{prefix, host, "https://" + host + "/", anchor})
	}
	return out
}

func NormalizeHubDate(raw string) string {
	raw = legacyTrim(raw)
	if len(raw) == 8 {
		raw = raw[:4] + "-" + raw[4:6] + "-" + raw[6:]
	}
	if len(raw) != 10 {
		return ""
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil || t.Year() < 1 {
		return ""
	}
	return t.Format("2006-01-02")
}

var hubDateRoute = regexp.MustCompile(`(?i)^/link/([0-9]{8}|[0-9]{4}-[0-9]{2}-[0-9]{2})/([0-9]{1,4})/?$`)

// ParseHubDateRoute does not apply route_prefix or the configurable trigger.
func ParseHubDateRoute(path string, maxNumber int) (date string, number int, ok bool) {
	path = legacyUnescape(legacyURLPath(path), false)
	path = regexp.MustCompile(`/+`).ReplaceAllString(path, "/")
	m := hubDateRoute.FindStringSubmatch(path)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[2])
	date = NormalizeHubDate(m[1])
	if date == "" || n >= max(10, min(9999, maxNumber)) {
		return "", 0, false
	}
	return date, n, true
}

func hubPageURL(scheme, domain, path, lang string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return scheme + "://" + domain + path + sep + "lan=" + url.QueryEscape(lang)
}

func hubPageHref(path, lang string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "lan=" + url.QueryEscape(lang)
}

func HubDateNavigation(scheme, domain, lang, effectiveDate string, maxNumber int, hash string) []HubDateGroup {
	out := []HubDateGroup{}
	day, err := time.Parse("2006-01-02", effectiveDate)
	if err != nil {
		return out
	}
	maxNumber = max(10, min(9999, maxNumber))
	for i := 0; i < 7; i++ {
		d := day.AddDate(0, 0, -i)
		date := d.Format("2006-01-02")
		if date < "2026-05-14" {
			break
		}
		label := d.Format("20060102")
		group := HubDateGroup{Date: date, Label: label, Links: []HubDateLink{}}
		seen := map[int]bool{}
		for slot := 0; len(group.Links) < 10 && slot < 200; slot++ {
			seed := legacyLower(domain) + "|" + legacyLower(lang) + "|" + effectiveDate + "|" + date + "|" + hash + "|" + strconv.Itoa(slot)
			n := int(hubHex(sha1Hex(seed), 0, 8) % uint64(maxNumber))
			if seen[n] {
				continue
			}
			seen[n] = true
			group.Links = append(group.Links, HubDateLink{hubPageURL(scheme, domain, "/link/"+label+"/"+strconv.Itoa(n)+"/", lang), label + "-" + strconv.Itoa(n), n})
		}
		out = append(out, group)
	}
	return out
}

func (s HubSnapshot) resolveDate(requested, today string) string {
	date := NormalizeHubDate(requested)
	if date != "" && s.SnapshotMaxIDs[date] > 0 {
		return date
	}
	dates := []string{}
	for d := range s.SnapshotMaxIDs {
		if NormalizeHubDate(d) != "" {
			dates = append(dates, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if date != "" && s.MissingDateStrategy == "nearest" && len(dates) > 0 {
		for _, d := range dates {
			if d <= date {
				return d
			}
		}
		return dates[0]
	}
	if NormalizeHubDate(s.ActiveDate) != "" && s.SnapshotMaxIDs[s.ActiveDate] > 0 {
		return s.ActiveDate
	}
	if len(dates) > 0 {
		return dates[0]
	}
	return today
}

var hubHTTP = regexp.MustCompile(`(?i)^https?://`)
var hubTestAnchor = regexp.MustCompile(`(?i)^test\s+external\s+link\b`)

func hubURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func hubExternalCount(hash string, minCount, maxCount, poolSize int) int {
	if poolSize <= minCount {
		return poolSize
	}
	upper := min(maxCount, poolSize)
	if upper <= minCount {
		return upper
	}
	return minCount + int(hubHex(hash, 0, 8)%uint64(upper-minCount+1))
}

func normalizedRecommendTier(tier int) int {
	if tier < 0 {
		return 0
	}
	return tier
}

func (s HubSnapshot) filteredPoolSize(date string, filterTier int, filtered bool) int {
	if counts, ok := s.TierCounts[date]; ok {
		if filtered {
			return counts[normalizedRecommendTier(filterTier)]
		}
		total := 0
		for _, count := range counts {
			total += count
		}
		return total
	}
	maxID := s.SnapshotMaxIDs[date]
	poolSize := 0
	for id, record := range s.ExternalRecords {
		if id <= maxID && record.Enabled && hubHTTP.MatchString(record.TargetURL) && recommendTierMatches(record.RecommendTier, filterTier, filtered) {
			poolSize++
		}
	}
	return poolSize
}

func (s HubSnapshot) selectSnapshot(date, hash string, needed, filterTier int, filtered bool) []HubLink {
	maxID := s.SnapshotMaxIDs[date]
	out := []HubLink{}
	if needed <= 0 || maxID <= 0 {
		return out
	}
	selected := []HubExternalRecord{}
	seen := map[int]bool{}
	try := func(id int) {
		if seen[id] {
			return
		}
		seen[id] = true
		r, ok := s.ExternalRecords[id]
		if !ok || !r.Enabled || !hubHTTP.MatchString(r.TargetURL) || !recommendTierMatches(r.RecommendTier, filterTier, filtered) {
			return
		}
		r.ID = id
		if r.Domain == "" {
			if u, e := url.Parse(r.TargetURL); e == nil {
				r.Domain = u.Hostname()
			}
		}
		selected = append(selected, r)
	}
	start := int(hubHex(sha1Hex(date+"|start|"+hash), 0, 8)%uint64(maxID)) + 1
	step := int(hubHex(sha1Hex(date+"|step|"+hash), 0, 8)%uint64(max(1, maxID-1))) + 1
	if step%2 == 0 {
		step++
	}
	for i, cursor := 0, start; i < min(maxID, needed*20+1000) && len(selected) < needed; i, cursor = i+1, cursor+step {
		try((cursor-1)%maxID + 1)
	}
	for id := 1; id <= maxID && len(selected) < needed; id++ {
		try(id)
	}
	score := func(r HubExternalRecord) string {
		return sha1Hex(date + "|" + hash + "|" + strconv.Itoa(r.ID) + "|" + r.TargetURL)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		a, b := score(selected[i]), score(selected[j])
		if a != b {
			return a > b
		}
		return selected[i].Weight > selected[j].Weight
	})
	for _, r := range selected {
		out = append(out, HubLink{Anchor: r.AnchorText, URL: r.TargetURL, Domain: r.Domain, RecommendTier: normalizedRecommendTier(r.RecommendTier)})
	}
	return out
}

func hubSelectLinks(hash, date, today string, cfg HubConfig, legacy LegacySnapshot, s HubSnapshot, recommendTier int, recommendFiltered bool) (left, bottom []HubLink, snapshotDate string, poolSize int) {
	resolved := s.resolveDate(date, today)
	fromSnapshot := s.SnapshotMaxIDs[resolved] > 0
	var valid []LegacyManagedLink
	if fromSnapshot {
		snapshotDate = resolved
		poolSize = s.filteredPoolSize(resolved, recommendTier, recommendFiltered)
	} else {
		rows := append([]LegacyManagedLink(nil), legacy.ExternalLinks...)
		if legacy.ExternalLinks == nil {
			for _, r := range legacy.Links {
				if r.Kind == "external" {
					rows = append(rows, legacyManagedFromSite(r))
				}
			}
		}
		// Apply the request filter before the PHP-compatible 200-row ceiling.
		rows = legacyExternalRowsFiltered(rows, 200, recommendTier, recommendFiltered)
		valid = []LegacyManagedLink{}
		for _, r := range rows {
			if hubHTTP.MatchString(r.TargetURL) {
				valid = append(valid, r)
			}
		}
		poolSize = len(valid)
	}
	countHash := hash
	if snapshotDate != "" {
		countHash += "|" + snapshotDate
	}
	n := min(poolSize, hubExternalCount(countHash, cfg.ExternalMin, cfg.ExternalMax, poolSize))
	totalTarget := min(poolSize, n+cfg.BottomLinkCount)
	var links []HubLink
	if fromSnapshot {
		links = s.selectSnapshot(resolved, hash, totalTarget, recommendTier, recommendFiltered)
	} else {
		sort.SliceStable(valid, func(i, j int) bool {
			score := func(r LegacyManagedLink) uint64 {
				return hubHex(sha1Hex(hash+"|"+r.TargetURL+"|"+r.Domain), 0, 12)*1001 + uint64(max(0, min(1000, r.Weight)))
			}
			a, b := score(valid[i]), score(valid[j])
			if a != b {
				return a > b
			}
			return valid[i].TargetURL < valid[j].TargetURL
		})
		links = make([]HubLink, 0, min(totalTarget, len(valid)))
		for _, r := range valid[:min(totalTarget, len(valid))] {
			links = append(links, HubLink{Anchor: r.AnchorText, URL: r.TargetURL, Domain: r.Domain, RecommendTier: normalizedRecommendTier(r.RecommendTier)})
		}
	}
	for i := range links {
		if !hubTestAnchor.MatchString(legacyTrim(links[i].Anchor)) {
			continue
		}
		d := legacyTrim(links[i].Domain)
		if d == "" {
			if u, e := url.Parse(links[i].URL); e == nil {
				d = u.Hostname()
			}
		}
		if d == "" {
			d = "external resource"
		}
		links[i].Anchor = d
	}
	left = append([]HubLink{}, links[:min(n, len(links))]...)
	bottom = append([]HubLink{}, links[min(n, len(links)):]...)
	return
}
