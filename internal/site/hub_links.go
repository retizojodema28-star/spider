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
}

type HubExternalRecord struct {
	ID         int    `json:"id"`
	Domain     string `json:"domain"`
	AnchorText string `json:"anchor_text"`
	TargetURL  string `json:"target_url"`
	Weight     int    `json:"weight"`
	Enabled    bool   `json:"enabled"`
}

type HubLink struct {
	Anchor string `json:"anchor"`
	URL    string `json:"url"`
	Domain string `json:"domain,omitempty"`
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

func (s HubSnapshot) selectSnapshot(date, hash string, needed int) []HubLink {
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
		if !ok || !r.Enabled || !hubHTTP.MatchString(r.TargetURL) {
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
		out = append(out, HubLink{r.AnchorText, r.TargetURL, r.Domain})
	}
	return out
}

func hubSelectLinks(hash, date, today string, cfg HubConfig, legacy LegacySnapshot, s HubSnapshot) (left, bottom []HubLink, snapshotDate string, poolSize int) {
	resolved := s.resolveDate(date, today)
	poolSize = s.SnapshotMaxIDs[resolved]
	var links []HubLink
	if poolSize > 0 {
		snapshotDate = resolved
		count := hubExternalCount(hash+"|"+resolved, cfg.ExternalMin, cfg.ExternalMax, poolSize)
		links = s.selectSnapshot(resolved, hash, min(poolSize, count+cfg.BottomLinkCount))
	} else {
		rows := append([]LegacyManagedLink(nil), legacy.ExternalLinks...)
		if legacy.ExternalLinks == nil {
			for _, r := range legacy.Links {
				if r.Kind == "external" {
					rows = append(rows, legacyManagedFromSite(r))
				}
			}
		}
		// Admin repository sorts and applies LIMIT 200 before validating URLs.
		rows = legacyExternalRows(rows, 200)
		valid := []LegacyManagedLink{}
		for _, r := range rows {
			if hubHTTP.MatchString(r.TargetURL) {
				valid = append(valid, r)
			}
		}
		score := func(r LegacyManagedLink) uint64 {
			return hubHex(sha1Hex(hash+"|"+r.TargetURL+"|"+r.Domain), 0, 12)*1001 + uint64(max(0, min(1000, r.Weight)))
		}
		sort.SliceStable(valid, func(i, j int) bool {
			a, b := score(valid[i]), score(valid[j])
			if a != b {
				return a > b
			}
			return valid[i].TargetURL < valid[j].TargetURL
		})
		for _, r := range valid {
			links = append(links, HubLink{r.AnchorText, r.TargetURL, r.Domain})
		}
		poolSize = len(valid)
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
	countHash := hash
	if snapshotDate != "" {
		countHash += "|" + snapshotDate
	}
	n := min(len(links), hubExternalCount(countHash, cfg.ExternalMin, cfg.ExternalMax, poolSize))
	left = append([]HubLink{}, links[:n]...)
	bottom = append([]HubLink{}, links[n:min(len(links), n+cfg.BottomLinkCount)]...)
	return
}
