package site

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ErrNotHubRoute = errors.New("request is not an enabled hub route")

type HubArticle struct {
	Heading string   `json:"heading"`
	Lead    string   `json:"lead"`
	Link    *HubLink `json:"link"`
	Tail    string   `json:"tail"`
}
type HubResource struct {
	Keyword   string `json:"keyword"`
	Anchor    string `json:"anchor"`
	URL       string `json:"url"`
	Domain    string `json:"domain"`
	Summary   string `json:"summary"`
	Paragraph string `json:"paragraph"`
	Website   string `json:"website"`
	Image     string `json:"image"`
}

func (r HubResource) Href() template.HTMLAttr        { return legacyHref(r.URL) }
func (r HubResource) WebsiteHref() template.HTMLAttr { return legacyHref(r.Website) }

type HubImages struct {
	Primary     string   `json:"primary"`
	Screenshots []string `json:"screenshots"`
	Alt         string   `json:"alt"`
}
type HubAlternate struct{ Language, URL string }

func (a HubAlternate) Href() template.HTMLAttr { return legacyHref(a.URL) }

type HubPage struct {
	StatusCode                                                   int
	Config                                                       HubConfig
	Scheme, Domain, RequestHost, Path, Language, Hash            string
	Brand, Title, Description, IntroText, PageURL, CanonicalHref string
	RequestedDate, NavigationDate, SnapshotDate                  string
	PoolSize                                                     int
	Subdomains                                                   []HubSubdomain
	DateNavigation                                               []HubDateGroup
	LeftLinks, BottomLinks                                       []HubLink
	ArticleBlocks, LeadArticles, RestArticles                    []HubArticle
	ResourceCards                                                []HubResource
	FAQ                                                          []LegacyFAQ
	RelatedPages, Breadcrumbs                                    []HubLink
	Labels, Sections                                             map[string]string
	Images                                                       HubImages
	Alternates                                                   []HubAlternate
	JSONLD                                                       []template.JS
	RecommendedHeading                                           string
	RecommendedHTML                                              template.HTML
}

func (p HubPage) Href(u string) template.HTMLAttr {
	return legacyHref(relativeSameHostURL(u, p.RequestHost, p.Domain))
}
func (HubPage) AbsoluteHref(u string) template.HTMLAttr { return legacyHref(u) }

type hubCopy struct {
	I18n     map[string]string `json:"i18n"`
	Intro    []string          `json:"intro"`
	Sections map[string]string `json:"sections"`
	Pack     struct {
		Related []string     `json:"related"`
		Article []HubArticle `json:"article"`
		FAQ     []LegacyFAQ  `json:"faq"`
	} `json:"pack"`
	Resource struct {
		Sentences  []string `json:"sentences"`
		Paragraphs []string `json:"paragraphs"`
	} `json:"resource"`
}

var hubCopies = func() map[string]hubCopy {
	var packs map[string]hubCopy
	if err := json.Unmarshal([]byte(hubCopyJSON), &packs); err != nil {
		panic(err)
	}
	return packs
}()
var hubBrandPath = regexp.MustCompile(`(?i)/([a-z0-9-]+)apk`)
var hubSlugPattern = regexp.MustCompile(`(?i)[^a-z0-9]+`)
var hubTLD = regexp.MustCompile(`(?i)\.[a-z0-9-]+$`)
var hubWebsite = regexp.MustCompile(`(?i)^https?://[^\s<>"']+$`)

// BuildHubPage resolves public hub requests using snapshot inputs only. It
// deliberately does not read User-Agent, use Redis/SQL, or request any APK.
// ErrNotHubRoute is the only signal to fall through to the detail renderer.
func BuildHubPage(r *http.Request, config SiteConfig, legacy LegacySnapshot, snapshot HubSnapshot) (HubPage, error) {
	if r == nil {
		return HubPage{}, errors.New("hub render requires a request")
	}
	if legacy.LanguageBank == nil {
		legacy.LanguageBank = &LanguageBank{}
	}
	req := legacyRequest(r, config, legacy)
	if !ShouldHandleHub(req.NormalizedPath, snapshot.Config) {
		return HubPage{}, ErrNotHubRoute
	}
	cfg := hubConfig(snapshot.Config)
	lang := req.RequestedLanguage
	if _, ok := hubCopies[lang]; !ok {
		lang = "en"
	}
	hash := sha1Hex(req.Scheme + "|" + req.Host + "|" + lang + "|" + req.EntryHost + "|" + req.HashURI)
	brand := "APK Hub"
	if m := hubBrandPath.FindStringSubmatch(req.NormalizedPath); m != nil && strings.Trim(m[1], "-") != "" {
		brand = strings.ToUpper(strings.ReplaceAll(strings.Trim(m[1], "-"), "-", " "))
	} else {
		names := []string{}
		for _, name := range legacy.AppNames {
			if name != "" && name != "0" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			brand = names[int(hubHex(hash, 0, 8)%uint64(len(names)))]
		}
	}
	wordLang := legacy.LanguageBank.legacyEffectiveLanguage(lang, legacy.FallbackLanguage, config.DefaultLanguage)
	words := legacy.LanguageBank.Words(wordLang, hash, 1, 0)
	if len(words) == 0 {
		words = legacyPickWords(legacy.GlobalWords, hash, 1)
	}
	if len(words) > 0 && legacyTrim(words[0]) != "" {
		brand = legacyTrim(words[0])
	}
	brand = legacyTrim(brand)
	if brand == "" {
		brand = "APK"
	}
	now := legacy.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	date := ""
	q := legacyQuery(req.RequestURI)
	for _, key := range []string{"date", "d", "day"} {
		if v, ok := q[key]; ok {
			date = NormalizeHubDate(v)
			break
		}
	}
	if d, _, ok := ParseHubDateRoute(req.NormalizedPath, cfg.DateNavNumMax); ok {
		date = d
	}
	images := hubLocalImages(req, config, legacy, brand, hash)
	recommendTier, recommendFiltered := recommendTierFromRequest(r)
	page := buildHubPage(req.Scheme, req.Host, req.NormalizedPath, lang, hash, brand, date, now, cfg, legacy, snapshot, images, recommendTier, recommendFiltered)
	page.RequestHost = r.Host
	return page, nil
}

func buildHubPage(scheme, domain, path, lang, hash, brand, date string, now time.Time, cfg HubConfig, legacy LegacySnapshot, snapshot HubSnapshot, images HubImages, recommendTier int, recommendFiltered bool) HubPage {
	if normalized := NormalizeHubDomain(domain); normalized != "" {
		domain = normalized
	}
	copy := hubCopies[lang]
	p := HubPage{StatusCode: http.StatusOK, Config: cfg, Scheme: scheme, Domain: domain, Path: path, Language: lang, Hash: hash, Brand: brand, RequestedDate: date, Labels: map[string]string{}, Sections: map[string]string{}, Alternates: []HubAlternate{}, RelatedPages: []HubLink{}, Breadcrumbs: []HubLink{}, ArticleBlocks: []HubArticle{}, ResourceCards: []HubResource{}, FAQ: []LegacyFAQ{}}
	p.Title = legacyTrim(brand + copy.I18n["page.title_suffix"])
	if items := legacy.TitleSuffixes[lang]; len(items) > 0 {
		suffix := legacyTrim(items[int(hubHex(sha1Hex(legacyLower(lang)+"|title_suffix|"+hash), 0, 8)%uint64(len(items)))])
		if suffix != "" {
			p.Title = hubTitle(suffix, brand)
		}
	}
	for key, value := range copy.I18n {
		p.Labels[strings.ReplaceAll(key, ".", "_")] = strings.Replace(value, "%s", brand, 1)
	}
	p.Description = p.Labels["page_description"]
	for key, value := range copy.Sections {
		p.Sections[key] = strings.ReplaceAll(value, "{brand}", brand)
	}
	p.PageURL = hubPageURL(scheme, domain, path, lang)
	// Relative canonical: a reverse proxy that serves this HTML from A.com
	// resolves it to A's URL instead of pointing back to B.
	p.CanonicalHref = hubPageHref(path, lang)
	for _, language := range []string{"en", "th", "id", "vn", "x-default"} {
		target := language
		if target == "x-default" {
			target = "en"
		}
		p.Alternates = append(p.Alternates, HubAlternate{language, hubPageURL(scheme, domain, path, target)})
	}
	today := now.Format("2006-01-02")
	p.LeftLinks, p.BottomLinks, p.SnapshotDate, p.PoolSize = hubSelectLinks(hash, date, today, cfg, legacy, snapshot, recommendTier, recommendFiltered)
	p.Subdomains = HubSubdomainNavigation(domain, cfg.SubdomainCount, hash, snapshot.Subdomains[NormalizeHubDomain(domain)])
	p.NavigationDate = NormalizeHubDate(date)
	if p.NavigationDate == "" {
		p.NavigationDate = today
	}
	p.DateNavigation = HubDateNavigation(scheme, domain, lang, p.NavigationDate, cfg.DateNavNumMax, hash)
	all := append(append([]HubLink{}, p.LeftLinks...), p.BottomLinks...)
	replace := strings.NewReplacer("{brand}", brand, "{year}", now.Format("2006"), "{count}", fmt.Sprint(len(all)+len(p.Subdomains)))
	if len(copy.Intro) > 0 {
		p.IntroText = replace.Replace(copy.Intro[int(hubHex(hash, 24, 8)%uint64(len(copy.Intro)))])
	}
	p.Images = images
	contentDate := p.SnapshotDate
	if contentDate == "" {
		contentDate = today
	}
	if n := len(copy.Pack.Article); n > 0 {
		offset := int(hubHex(hash, 8, 8) % uint64(n))
		for i := 0; i < min(5, n); i++ {
			a := copy.Pack.Article[(offset+i)%n]
			d := brand
			if i < len(all) {
				link := all[i]
				if link.Domain != "" {
					d = link.Domain
				}
				link.Anchor = map[string]string{"id": "cek di sini", "en": "check here", "th": "ตรวจสอบที่นี่", "vn": "kiem tra tai day"}[lang]
				a.Link = &link
			}
			repl := strings.NewReplacer("{brand}", brand, "{domain}", d, "{date}", contentDate)
			a.Heading = strings.ReplaceAll(a.Heading, "{brand}", brand)
			a.Lead = repl.Replace(a.Lead)
			a.Tail = repl.Replace(a.Tail)
			p.ArticleBlocks = append(p.ArticleBlocks, a)
		}
	}
	p.LeadArticles = p.ArticleBlocks[:min(3, len(p.ArticleBlocks))]
	p.RestArticles = p.ArticleBlocks[min(3, len(p.ArticleBlocks)):]
	for i, link := range all[:min(12, len(all))] {
		image := p.Images.Primary
		if len(p.Images.Screenshots) > 0 {
			image = p.Images.Screenshots[i%len(p.Images.Screenshots)]
		}
		p.ResourceCards = append(p.ResourceCards, hubResource(copy, link, brand, hash, image))
	}
	for _, faq := range copy.Pack.FAQ {
		repl := strings.NewReplacer("{brand}", brand, "{date}", contentDate)
		p.FAQ = append(p.FAQ, LegacyFAQ{Question: repl.Replace(faq.Question), Answer: repl.Replace(faq.Answer)})
	}
	for i, suffix := range []string{"resmi", "apk", "masuk", "unduh", "lokasi", "diskusi"} {
		p.RelatedPages = append(p.RelatedPages, HubLink{Anchor: strings.ReplaceAll(copy.Pack.Related[i], "{brand}", brand), URL: hubPageHref("/"+hubSlug(brand)+suffix+"/", lang)})
	}
	for _, crumb := range [][2]string{{"Home", "/"}, {"APK Hub", "/apk/"}, {brand, "/" + hubSlug(brand) + "apk/"}} {
		p.Breadcrumbs = append(p.Breadcrumbs, HubLink{Anchor: crumb[0], URL: hubPageHref(crumb[1], lang)})
	}
	hubJSONLD(&p)
	return p
}

func hubTitle(suffix, brand string) string {
	if strings.Contains(suffix, "{keyword}") || strings.Contains(suffix, "{category}") {
		return legacyTrim(strings.NewReplacer("{keyword}", brand, "{category}", "").Replace(suffix))
	}
	if strings.Contains(suffix, "%s") {
		suffix = strings.Replace(suffix, "%s", brand, 1)
		suffix = strings.Replace(suffix, "%s", "", 1)
		return legacyTrim(strings.ReplaceAll(suffix, "%%", "%"))
	}
	return legacyTrim(brand + " " + suffix)
}
func hubSlug(s string) string {
	s = strings.Trim(legacyLower(hubSlugPattern.ReplaceAllString(s, "-")), "-")
	if s == "" {
		return "apk"
	}
	return s
}
func hubResource(copy hubCopy, link HubLink, brand, hash, image string) HubResource {
	domain := link.Domain
	if domain == "" {
		domain = hubURLHost(link.URL)
	}
	keyword := legacyTrim(link.Anchor)
	if keyword == "" {
		if domain != "" {
			keyword = hubTLD.ReplaceAllString(domain, "")
		} else {
			keyword = link.URL
		}
	}
	keyword = keyword[:min(120, len(keyword))]
	repl := strings.NewReplacer("{keyword}", keyword, "{brand}", brand, "{domain}", domain)
	pick := func(field string, items []string) string {
		if len(items) == 0 {
			return ""
		}
		return repl.Replace(items[int(hubHex(sha1Hex(hash+"|"+field+"|"+link.URL), 0, 8)%uint64(len(items)))])
	}
	website := legacyTrim(link.URL)
	if !hubWebsite.MatchString(website) {
		website = ""
	}
	return HubResource{Keyword: keyword, Anchor: keyword, URL: link.URL, Domain: domain, Summary: pick("summary", copy.Resource.Sentences), Paragraph: pick("paragraph", copy.Resource.Paragraphs), Website: website, Image: image}
}

func hubJSONLD(p *HubPage) {
	items := []any{}
	for _, r := range p.ResourceCards {
		item := map[string]any{"@type": "ListItem", "position": len(items) + 1, "name": r.Keyword, "url": r.URL}
		if r.Image != "" {
			item["image"] = r.Image
		}
		items = append(items, item)
	}
	for _, l := range p.LeftLinks[:min(len(p.LeftLinks), max(0, 20-len(p.ResourceCards)))] {
		items = append(items, map[string]any{"@type": "ListItem", "position": len(items) + 1, "name": l.Anchor, "url": l.URL})
	}
	crumbs := []any{}
	for i, c := range p.Breadcrumbs {
		item := c.URL
		if strings.HasPrefix(item, "/") {
			item = p.Scheme + "://" + p.Domain + item
		}
		crumbs = append(crumbs, map[string]any{"@type": "ListItem", "position": i + 1, "name": c.Anchor, "item": item})
	}
	faq := []any{}
	for _, f := range p.FAQ {
		faq = append(faq, map[string]any{"@type": "Question", "name": f.Question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": f.Answer}})
	}
	for _, data := range []any{map[string]any{"@context": "https://schema.org", "@type": "CollectionPage", "name": p.Title, "inLanguage": p.Language, "url": p.PageURL, "description": p.Description, "image": p.Images.Screenshots, "mainEntity": map[string]any{"@type": "ItemList", "numberOfItems": len(items), "itemListElement": items}}, map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": crumbs}, map[string]any{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": faq}} {
		// Escape HTML delimiters while retaining identical parsed JSON values.
		b, _ := json.Marshal(data)
		p.JSONLD = append(p.JSONLD, template.JS(b))
	}
}

// RenderHubPage also supports callers without the server's pageRenderer.
func RenderHubPage(w io.Writer, page HubPage) error {
	t, err := template.ParseFS(templateFiles, "templates/hub.html")
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, "hub.html", page)
}
