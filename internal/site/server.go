package site

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

//go:embed templates/*.html
var templateFiles embed.FS

type pageRenderer struct{ templates *template.Template }

// Render into a buffer before writing headers, never return a partial 200 on a
// template error. HEAD retains the same headers but has no response body.
func (p pageRenderer) render(c *gin.Context, name string, data any) {
	var body bytes.Buffer
	if err := p.templates.ExecuteTemplate(&body, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Length", strconv.Itoa(body.Len()))
	c.Status(http.StatusOK)
	if c.Request.Method != http.MethodHead {
		_, _ = c.Writer.Write(body.Bytes())
	}
}

type PublicPage struct {
	PageType        string
	Title           string
	Paragraphs      []string
	Config          SiteConfig
	Canonical       string
	LegacyBaseURL   string
	RequestHost     string
	Apps            []App
	RelatedApps     []App
	InternalLinks   []SiteLink
	ExternalLinks   []SiteLink
	NavigationLinks []SiteLink
}

func (p PublicPage) Href(raw string) template.HTMLAttr {
	return legacyHref(relativeSameHostURL(raw, p.RequestHost))
}

func (PublicPage) AbsoluteHref(raw string) template.HTMLAttr { return legacyHref(raw) }

type DetailPage struct {
	Config        SiteConfig
	Canonical     string
	LegacyBaseURL string
	RequestHost   string
	App           App
	KeywordBrand  string
	KeywordURL    string
	KeywordChips  []string
	RelatedApps   []App
	InternalLinks []SiteLink
	ExternalLinks []SiteLink
}

func (p DetailPage) Href(raw string) template.HTMLAttr {
	return legacyHref(relativeSameHostURL(raw, p.RequestHost))
}

func (DetailPage) AbsoluteHref(raw string) template.HTMLAttr { return legacyHref(raw) }

type RouterOptions struct {
	AssetRoot          string
	Admin              *AdminOptions
	RecommendationFeed *RecommendationFeed
}

// Compatibility constructors for embedding/tests; production explicitly starts
// the runtime's revision watcher in cmd/site. Initial failures are fail-closed.
func NewRouter(db *gorm.DB) *gin.Engine { return NewRouterWithLanguageBank(db, nil) }
func NewRouterWithLanguageBank(db *gorm.DB, bank *LanguageBank) *gin.Engine {
	var err error
	if bank == nil {
		bank, err = LoadLanguageBank(envOr("LANGUAGE_DATA_DIR", "data/lan"), "id")
		if err != nil {
			panic(err)
		}
	}
	if err = MigrateAdmin(db); err != nil {
		panic(err)
	}
	runtime, err := NewRuntime(context.Background(), db, RuntimeOptions{})
	if err != nil {
		panic(err)
	}
	runtime.Snapshot().Bank = bank // before publishing the router to any caller
	return NewRouterWithRuntime(runtime, RouterOptions{})
}

func NewRouterWithRuntime(runtime *Runtime, opts RouterOptions) *gin.Engine {
	renderer := pageRenderer{templates: template.Must(template.New("pages").Funcs(template.FuncMap{
		"requestImage": func(path string) string { return "/assets/" + strings.TrimPrefix(path, "/") },
		"json":         func(value any) template.JS { data, _ := json.Marshal(value); return template.JS(data) },
	}).ParseFS(templateFiles, "templates/*.html"))}
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery(), func(c *gin.Context) { c.Header("X-Content-Type-Options", "nosniff"); c.Next() })
	health := func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		conn, err := runtime.db.DB()
		if err != nil || conn.PingContext(ctx) != nil || runtime.Snapshot() == nil {
			c.JSON(503, gin.H{"ok": false})
			return
		}
		c.JSON(200, gin.H{"ok": true, "revision": runtime.Snapshot().Revision})
	}
	router.GET("/healthz", health)
	missingSpecialFile := func(c *gin.Context) {
		c.Data(http.StatusNotFound, "text/plain; charset=utf-8", nil)
	}
	for _, specialPath := range []string{"/robots.txt", "/sitemap.xml"} {
		router.GET(specialPath, missingSpecialFile)
		router.HEAD(specialPath, missingSpecialFile)
	}
	if opts.AssetRoot != "" {
		router.Static("/assets", opts.AssetRoot)
	}
	if opts.Admin != nil {
		if err := RegisterAdminRoutes(router, runtime, *opts.Admin); err != nil {
			panic(err)
		}
	}

	// Samples are an explicit noindex fixture preview, not an alternative
	// implementation of public /apps/* content or links.
	router.GET("/__preview/", func(c *gin.Context) {
		s := runtime.Snapshot()
		if s == nil {
			c.Status(503)
			return
		}
		c.Header("X-Robots-Tag", "noindex, nofollow")
		renderer.render(c, "index.html", PublicPage{Config: s.Config, Apps: s.ActiveApps(), Canonical: phpCanonicalURL(c.Request.Host, c.Request.URL.Path), LegacyBaseURL: phpPublicBaseURL(requestScheme(c), c.Request.Host), RequestHost: c.Request.Host})
	})
	router.GET("/__preview/apps/:slug", func(c *gin.Context) {
		s := runtime.Snapshot()
		if s == nil {
			c.Status(503)
			return
		}
		app, ok := s.FindApp(c.Param("slug"))
		if !ok {
			c.Status(404)
			return
		}
		c.Header("X-Robots-Tag", "noindex, nofollow")
		renderer.render(c, "detail.html", DetailPage{Config: s.Config, App: app, RelatedApps: s.ActiveApps(), Canonical: phpCanonicalURL(c.Request.Host, c.Request.URL.Path), LegacyBaseURL: phpPublicBaseURL(requestScheme(c), c.Request.Host), RequestHost: c.Request.Host, InternalLinks: s.LinksFor("preview", "internal", app.Slug, app.Language, c.Request.Host), ExternalLinks: s.LinksFor("preview", "external", app.Slug, app.Language, c.Request.Host)})
	})
	public := func(c *gin.Context) {
		path := strings.ToLower(c.Request.URL.Path)
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			c.Status(404)
			return
		}
		for _, prefix := range []string{"/admin", "/assets", "/healthz", "/__preview", "/api", "/.git", "/.env"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				c.Status(404)
				return
			}
		}
		// Only the three known PHP health paths may fall through to public
		// HTML (encoded/case variants or explicit home fallback). Keep the
		// rest of /health reserved; query overrides never select a handler.
		healthPath := path == "/health/live" || path == "/health/ready" || path == "/health/startup"
		if (path == "/health" || strings.HasPrefix(path, "/health/")) && !healthPath {
			c.Status(404)
			return
		}
		raw := c.Request.RequestURI
		if raw == "" {
			raw = c.Request.URL.RequestURI()
		}
		if legacyQuery(raw)["__apk_home_fallback"] != "1" {
			rawPath := legacyURLPath(raw)
			switch rawPath {
			case "/health/live", "/health/ready", "/health/startup":
				// Retain the existing GET-only operational endpoint boundary.
				if c.Request.Method != http.MethodGet {
					c.Status(404)
					return
				}
				if rawPath == "/health/live" {
					c.String(200, "ok\n")
				} else {
					health(c)
				}
				return
			}
		}
		s := runtime.Snapshot()
		if s == nil {
			c.Status(503)
			return
		}
		cfg := s.ConfigForHost(c.Request.Host)
		snapshot := publicSnapshot(s, cfg)
		snapshot.LocalImageFiles = s.LocalImageFiles
		snapshot.MaxRelatedLinks = &cfg.MaxRelatedLinks
		snapshot.ShowListPanel = cfg.ShowListPanel
		snapshot.ShowSubdomainPanel = cfg.ShowSubdomainPanel
		snapshot.ShowExternalPanel = cfg.ShowExternalPanel
		hubPage, hubErr := BuildHubPage(c.Request, cfg, snapshot, s.Hub)
		if hubErr == nil {
			if recommendationFeedEnabled(c.Request) && opts.RecommendationFeed != nil {
				fragment, feedErr := opts.RecommendationFeed.Fetch(c.Request.Context(), hubPage.Domain, recommendationRequestPath(c.Request))
				if feedErr != nil {
					log.Printf("hub recommendation feed skipped: %v", feedErr)
				} else if fragment != "" {
					hubPage.RecommendedHeading = recommendationHeading(hubPage.Language)
					hubPage.RecommendedHTML = fragment
				}
			}
			c.Header("X-Runtime-Revision", strconv.FormatUint(s.Revision, 10))
			renderer.render(c, "hub.html", hubPage)
			return
		}
		if !errors.Is(hubErr, ErrNotHubRoute) {
			log.Printf("build public hub page: %v", hubErr)
			c.Status(500)
			return
		}
		page, err := BuildLegacyPage(c.Request, cfg, snapshot)
		if err != nil {
			log.Printf("build public page: %v", err)
			c.Status(500)
			return
		}
		if recommendationFeedEnabled(c.Request) && opts.RecommendationFeed != nil {
			fragment, feedErr := opts.RecommendationFeed.Fetch(c.Request.Context(), page.Host, recommendationRequestPath(c.Request))
			if feedErr != nil {
				log.Printf("recommendation feed skipped: %v", feedErr)
			} else if fragment != "" {
				page.RecommendedHeading = recommendationHeading(page.Language)
				page.RecommendedHTML = fragment
			}
		}
		c.Header("X-Runtime-Revision", strconv.FormatUint(s.Revision, 10))
		renderer.render(c, "legacy.html", page)
	}
	// Gin matches decoded URL.Path. The shared dispatcher rechecks the raw
	// PHP path without rewriting the request or entering admin/static routes.
	router.GET("/health/live", public)
	router.GET("/health/ready", public)
	router.GET("/health/startup", public)
	router.NoRoute(public)
	return router
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func requestScheme(c *gin.Context) string {
	return legacyRequestScheme(c.Request)
}
func sortLinks(links []SiteLink) {
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Weight == links[j].Weight {
			return links[i].ID < links[j].ID
		}
		return links[i].Weight > links[j].Weight
	})
}

// Site keys are the Go persistence namespace, not PHP language/domain filters.
// Generic seed examples remain in the explicit preview namespace.
func publicSnapshot(s *Snapshot, cfg SiteConfig) LegacySnapshot {
	return LegacySnapshotFromRuntimeForSite(s, cfg.SiteKey)
}
