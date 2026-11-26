package site

import (
	"context"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	db := testSiteDB(t)
	if err := MigrateAdmin(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, "../../data/seed/apps.json"); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(context.Background(), db, RuntimeOptions{LanguageRoot: "../../data/lan"})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestPublicSnapshotSurvivesDatabaseOutage(t *testing.T) {
	rt := testRuntime(t)
	router := NewRouterWithRuntime(rt, RouterOptions{AssetRoot: "../../assets"})
	queries := 0
	if err := rt.db.Callback().Query().Before("gorm:query").Register("test_public_no_queries", func(db *gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	fetch := func(method, path, ua string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Host = "preview.example.test:18443"
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	a := fetch("GET", "/apps/not-in-database?lan=id", "Mozilla/5.0")
	b := fetch("GET", "/apps/not-in-database?lan=id", "Googlebot")
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatal("public route/UA differs")
	}
	if queries != 0 {
		t.Fatalf("public requests queried database %d times", queries)
	}
	conn, _ := rt.db.DB()
	_ = conn.Close()
	if w := fetch("GET", "/healthz", ""); w.Code != 503 {
		t.Fatalf("health after outage=%d", w.Code)
	}
	if w := fetch("GET", "/apps/not-in-database?lan=id", ""); w.Code != 200 || w.Body.String() != a.Body.String() {
		t.Fatal("snapshot not available during database outage")
	}
	if err := rt.Reload(context.Background()); err == nil {
		t.Fatal("reload unexpectedly worked with closed DB")
	}
	if w := fetch("GET", "/apps/not-in-database?lan=id", ""); w.Code != 200 || w.Body.String() != a.Body.String() {
		t.Fatal("failed reload replaced last known good snapshot")
	}
}

func TestPublicSnapshotConfigReloadAndReservedRoutes(t *testing.T) {
	rt := testRuntime(t)
	router := NewRouterWithRuntime(rt, RouterOptions{AssetRoot: "../../assets"})
	if err := UpdateRuntime(context.Background(), rt.db, func(tx *gorm.DB) error {
		return tx.Model(&SiteConfig{}).Where("site_key = ?", "default").Update("route_prefix", "test").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.ReloadIfChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/testapps/demo?lan=id", nil)
	r.Host = "example.com:18443"
	router.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `href="/testcategory/game/"`) {
		t.Fatal("updated route prefix not reflected in public navigation")
	}
	for _, path := range []string{"/admin/api/stats/summary", "/assets/missing.png", "/.env", "/.git/config", "/__preview/missing"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("reserved %s = %d", path, w.Code)
		}
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/__preview/apps/sample-app-one", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatal("sample preview must be noindex")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("HEAD", "/anything?lan=id", nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD representation headers/body mismatch")
	}
}

func TestRenderingErrorDoesNotWritePartialSuccess(t *testing.T) {
	renderer := pageRenderer{templates: template.Must(template.New("bad").Parse(`partial {{.DoesNotExist}}`))}
	router := gin.New()
	router.GET("/", func(c *gin.Context) { renderer.render(c, "bad", struct{}{}) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "partial") {
		t.Fatal("render returned partial success")
	}
}

func TestSampleLinksDoNotExposeRetiredHub(t *testing.T) {
	rt := testRuntime(t)
	s := rt.Snapshot()
	formal := publicSnapshot(s, s.Config)
	if len(formal.Links) != 0 {
		t.Fatal("sample fixture links leaked into formal PHP link collections")
	}
	if len(s.LinksFor("preview", "external", "sample-app-one", "id", "example.com")) == 0 {
		t.Fatal("sample links lost from explicit preview")
	}
	router := NewRouterWithRuntime(rt, RouterOptions{})
	req := httptest.NewRequest("GET", "/link/?lan=id", nil)
	req.Host = "preview.example.test:18443"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("retired live hub returned %d", w.Code)
	}
}
