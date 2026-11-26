package site

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LegacySnapshot is an immutable, already loaded public-rendering snapshot. The
// caller owns DB refreshes; BuildLegacyPage performs no IO and never reads a UA.
// Apps supply image fixtures only, even when their slug matches the request.
type LegacySnapshot struct {
	LanguageBank       *LanguageBank
	Apps               []App
	DomainBindings     []DomainBinding
	ImageBindings      []DomainImageBinding
	LocalImageFiles    map[string]bool // URL keys from IndexLegacyLocalImageFiles
	Links              []SiteLink
	InternalDomains    []LegacyManagedLink
	ExternalLinks      []LegacyManagedLink
	MainExternalLinks  []LegacyManagedLink
	TitleSuffixes      map[string][]string
	Categories         []string
	Hosts              []string
	AppNames           []string
	GlobalWords        []string
	FallbackLanguage   string
	MaxRelatedLinks    *int // nil = PHP's 6; a pointer preserves explicit zero
	ShowListPanel      bool
	ShowSubdomainPanel bool
	ShowExternalPanel  bool
	HubEnabled         bool
	HubTrigger         string    // default "link"; detection only, no hub aggregation
	EntryScripts       []string  // nil = /index.php and /799lh.php
	Preview            bool      // explicit preview routes only; sample images do not imply noindex
	Now                time.Time // optional injected clock, UTC by default like PHP
}

// LegacyManagedLink contains the normalized repository row. PHPJSON preserves
// the original object's field order for SHA1 sorting when importing PHP data;
// a Go map would sort the keys and silently change the selected links.
type LegacyManagedLink struct {
	ID            uint            `json:"id"`
	Domain        string          `json:"domain"`
	Title         string          `json:"title,omitempty"`
	Path          string          `json:"path,omitempty"`
	Language      string          `json:"lang"`
	AnchorText    string          `json:"anchor_text,omitempty"`
	TargetURL     string          `json:"target_url,omitempty"`
	Category      string          `json:"category"`
	RecommendTier int             `json:"recommend_tier"`
	Weight        int             `json:"weight"`
	Enabled       bool            `json:"enabled"`
	Remark        string          `json:"remark"`
	PHPJSON       json.RawMessage `json:"-"`
}

type LegacyLink struct {
	Title    string `json:"title"`
	Path     string `json:"path"`
	External bool   `json:"external,omitempty"`
	Source   string `json:"source,omitempty"`
	Pinned   int    `json:"pinned,omitempty"`
}

// Preserve PHP's literal href attribute (including Unicode/spacing). Escaping
// the full attribute avoids html/template's additional URL percent encoding.
func (l LegacyLink) Href() template.HTMLAttr { return legacyHref(l.Path) }
func (p LegacyPage) Href(raw string) template.HTMLAttr {
	return legacyHref(relativeSameHostURL(raw, p.RequestHost, p.EntryHost, p.Host))
}
func (LegacyPage) AbsoluteHref(raw string) template.HTMLAttr { return legacyHref(raw) }
func legacyHref(url string) template.HTMLAttr {
	return template.HTMLAttr(`href="` + html.EscapeString(url) + `"`)
}

// relativeSameHostURL keeps same-site anchors on the current browser origin,
// including local preview ports, while preserving genuinely cross-site URLs.
// Canonical/alternate metadata deliberately bypasses this helper.
func relativeSameHostURL(raw string, requestHosts ...string) string {
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" || target.User != nil ||
		(target.Scheme != "" && target.Scheme != "http" && target.Scheme != "https") {
		return raw
	}
	targetHost := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if targetHost == "" {
		return raw
	}
	matched := false
	for _, requestHost := range requestHosts {
		if requestHost == "" {
			continue
		}
		request, parseErr := url.Parse("//" + requestHost)
		if parseErr != nil {
			continue
		}
		requestName := strings.TrimSuffix(strings.ToLower(request.Hostname()), ".")
		if requestName != targetHost {
			continue
		}
		// Generated PHP navigation omits the port; let it inherit the browser's
		// current port. An explicit different port denotes another service and
		// must remain intact. Managed external links bypass this helper entirely.
		requestPort, requestHasPort := request.Port(), request.Port() != ""
		targetPort, targetHasPort := target.Port(), target.Port() != ""
		if targetHasPort && (!requestHasPort || requestPort != targetPort) {
			continue
		}
		matched = true
		break
	}
	if !matched {
		return raw
	}
	// Cut only the authority, preserving PHP's literal path/query/fragment,
	// including Unicode, percent escapes, empty '?' and empty '#'. Rebuilding
	// from url.URL would silently re-encode some of those bytes.
	authority := raw
	if target.Scheme != "" {
		authority = raw[strings.Index(raw, ":")+1:]
	}
	authority = strings.TrimPrefix(authority, "//")
	i := strings.IndexAny(authority, "/?#")
	if i < 0 {
		return "/"
	}
	path := authority[i:]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// A literal // or /\ prefix is an authority in browsers. Keep it a path
	// on this origin, rather than accidentally turning it into an external URL.
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, `/\`) {
		path = "/." + path
	}
	return path
}

type LegacyFAQ struct {
	Question string `json:"q"`
	Answer   string `json:"a"`
}

type LegacyVersion struct{ Version, Size, Date string }
type LegacyField struct{ Name, Value string }
type LegacyLinkGroup struct {
	Heading string
	Links   []LegacyLink
}
type LegacyImages struct {
	Primary     string
	Screenshots []string
	SEO         []string
}

type LegacyPage struct {
	StatusCode                                                                            int
	Config                                                                                SiteConfig
	Host, EntryHost, RequestHost, Scheme, RequestURI, HashURI                             string
	NormalizedPath, ContentPath, DisplayPath, RoutePrefix                                 string
	RequestedLanguage, Language, PageType, Hash, ContentHash                              string
	RecommendTierFilter                                                                   int
	RecommendFiltered                                                                     bool
	HomeFallback, HubRoute, Preview                                                       bool
	Title, Brand, Description                                                             string
	Keywords, Paragraphs                                                                  []string
	FAQ                                                                                   []LegacyFAQ
	Canonical, RawCanonical, BaseURL, SEOBaseURL                                          string
	HomeURL, SEOHomeURL, GameURL, CategoryURL, KeywordURL                                 string
	Navigation, RelatedLinks, ListLinks, SubdomainLinks, ExternalLinks, MainExternalLinks []LegacyLink
	KeywordChips                                                                          []string
	SocialLinks, LegalLinks                                                               []LegacyLink
	FooterGroups                                                                          []LegacyLinkGroup
	ShowListPanel, ShowSubdomainPanel, ShowExternalPanel                                  bool
	Labels                                                                                map[string]string
	Images                                                                                LegacyImages
	Version, FileSize, PackageName, SHA256                                                string
	OldVersions                                                                           []LegacyVersion
	Info                                                                                  []LegacyField
	UpdatedDate, UpdatedISO, Year                                                         string
	RatingValue                                                                           float64
	RatingText                                                                            string
	RatingCount                                                                           int
	DisplayTitle, SEOKeywords, OGLocale, Robots                                           string
	JSONLD                                                                                []template.JS
	RecommendedHeading                                                                    string
	RecommendedHTML                                                                       template.HTML
}

// BuildLegacyPage ports the default ApkDetailRenderer/SeoRenderer pipeline.
// Every public slug/category/home request is a generated page (200), including
// sample slugs. The caller must reserve admin, health, static assets and APKs.
// HubRoute identifies the separately scoped spider aggregator. If the parent
// elects to serve this model there, it is PHP's detail fallback, not a hub port.
func BuildLegacyPage(r *http.Request, config SiteConfig, snapshot LegacySnapshot) (LegacyPage, error) {
	if r == nil {
		return LegacyPage{}, errors.New("legacy render requires a request")
	}
	if snapshot.LanguageBank == nil {
		return LegacyPage{}, errors.New("legacy render requires a language-bank snapshot")
	}
	p := legacyRequest(r, config, snapshot)
	p.Config = config
	p.StatusCode = http.StatusOK
	p.Preview = snapshot.Preview
	p.Keywords = snapshot.LanguageBank.Words(p.Language, p.Hash, 80, 0)
	if len(p.Keywords) == 0 {
		p.Keywords = legacyPickWords(snapshot.GlobalWords, p.Hash, 80)
	}
	p.Brand = "android"
	if len(p.Keywords) > 0 {
		p.Brand = p.Keywords[0]
	}
	p.Title = legacyTitle(p.Brand, p.Hash, p.Language, snapshot)
	sentences := snapshot.LanguageBank.Sentences(p.Language, p.Hash, 8, 0)
	p.Paragraphs = legacyParagraphs(sentences, p.Title, p.Brand, p.Keywords)
	p.Description = p.Paragraphs[0]
	p.FAQ = []LegacyFAQ{
		{"Is this page stable?", "Yes. The normalized URL maps to a deterministic hash and produces stable content."},
		{"Does every request read MySQL?", "No. Public rendering uses local snapshots and in-memory lan by default."},
	}
	if len(sentences) > 4 {
		p.FAQ[0].Answer = sentences[4]
	}
	if len(sentences) > 5 {
		p.FAQ[1].Answer = sentences[5]
	}
	content := struct {
		Title       string      `json:"title"`
		Description string      `json:"description"`
		Brand       string      `json:"primary_keyword"`
		Paragraphs  []string    `json:"paragraphs"`
		FAQ         []LegacyFAQ `json:"faq"`
	}{p.Title, p.Description, p.Brand, p.Paragraphs, p.FAQ}
	digest := sha256.Sum256(legacyJSON(content))
	p.ContentHash = hex.EncodeToString(digest[:])
	p.Labels = legacyLabelsFor(p.Language).Detail
	// Share text must reflect the origin that served this request, including a
	// non-default preview port. p.Host is intentionally PHP-normalized for SEO
	// and has already had its port removed.
	shareHost := p.RequestHost
	if shareHost == "" {
		shareHost = p.Host
	}
	p.RawCanonical = p.Scheme + "://" + shareHost + p.DisplayPath
	p.Canonical = "https://" + p.Host + p.DisplayPath
	p.BaseURL = p.Scheme + "://" + p.Host
	p.SEOBaseURL = "https://" + p.Host
	p.HomeURL = p.internalURL("/", false)
	p.SEOHomeURL = p.internalURL("/", true)
	p.GameURL = p.internalURL("/category/game", false)
	p.CategoryURL = p.internalURL("/category/adventure", false)
	p.KeywordURL = ApplyLegacyPrefix("/apps/"+legacySlug(p.Brand), p.RoutePrefix)
	p.KeywordChips = phpKeywordChips(p.Keywords, p.Hash, p.Brand)
	legacyBuildLinks(&p, snapshot)
	legacyMetadata(&p, snapshot.Now)
	p.Images = legacyLocalImages(p, config, snapshot)
	legacyBuildSEO(&p)
	return p, nil
}

// Legacy JSON uses PHP's JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES.
// Go's HTML escaping would change content and managed-link hashes.
func legacyJSON(v any) []byte {
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return []byte(strings.TrimSuffix(b.String(), "\n"))
}

func legacyMetadata(p *LegacyPage, now time.Time) {
	seed := int(hashUint32(sha1Hex(p.Hash)))
	p.Version = fmt.Sprintf("6.%d.0_%d", seed%8+1, 30000000+seed%9000000)
	p.FileSize = fmt.Sprintf("%.1f MB", 28+float64(seed%6800)/10)
	p.PackageName = "com." + legacySlug(p.Brand) + ".android"
	digest := sha256.Sum256([]byte(p.Hash))
	p.SHA256 = hex.EncodeToString(digest[:])
	for i := 0; i < 4; i++ {
		date := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -20-i*42)
		p.OldVersions = append(p.OldVersions, LegacyVersion{
			fmt.Sprintf("6.%d.0_%d", max(0, 2-i), 30000000+seed%8000000-i*8317),
			fmt.Sprintf("%.1f MB", float64(420+seed%180-i*11)), date.Format("Jan 2, 2006"),
		})
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	p.UpdatedDate, p.UpdatedISO, p.Year = now.Format("Jan 2, 2006"), now.Format("2006-01-02"), now.Format("2006")
	p.RatingValue = float64(46+int(hashUint32(sha1Hex("rating-value|"+p.Hash)))%5) / 10
	p.RatingText = fmt.Sprintf("%.1f", p.RatingValue)
	p.RatingCount = 85000 + int(hashUint32(sha1Hex("rating-count|"+p.Hash)))%165000
	for _, row := range [][2]string{{"latest_version", p.Version}, {"info_category", p.Labels["category"]}, {"android_os", "Android 6.0+"}, {"file_size", p.FileSize}, {"developer_prefix", p.Brand}, {"available_on", "APK"}, {"content_rating", p.Labels["content_rating_value"]}, {"package_name", p.PackageName}} {
		p.Info = append(p.Info, LegacyField{p.Labels[row[0]], row[1]})
	}
}
