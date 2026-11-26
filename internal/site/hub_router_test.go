package site

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// Exercise the parent's actual NoRoute dispatch, not just the pure builder.
// Hub pages render from the loaded snapshot and must not query SQL per request.
func TestPublicHubRoutesAreServed(t *testing.T) {
	rt := testRuntime(t)
	router := NewRouterWithRuntime(rt, RouterOptions{AssetRoot: "../../assets"})
	queries := 0
	if err := rt.db.Callback().Query().Before("gorm:query").Register("hub_public_no_queries", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	fetch := func(method, target, ua string) *httptest.ResponseRecorder {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, u.RequestURI(), nil)
		r.Host = "hub.example.test"
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, target := range []string{
		"/link",
		"/link/",
		"/link/20261125/0/",
		"/?path=/link/",
		"/apps/example?path=/link/",
		"/799lh.php/link/?lan=id",
		"/router.php/link/?lan=id",
		"/index.php/link/?lan=id",
		"/LINK/",
	} {
		got := fetch("GET", target, "Mozilla/5.0")
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"@type":"CollectionPage"`) {
			t.Fatalf("Hub route %s returned %d without CollectionPage", target, got.Code)
		}
		if strings.Contains(got.Body.String(), "Tautan Mitra") || strings.Contains(got.Body.String(), "Partner Links") {
			t.Fatalf("Hub route %s still renders the partner-links sidebar", target)
		}
		if strings.Contains(got.Body.String(), "Sumber Eksternal Pilihan") || strings.Contains(got.Body.String(), "Selected External Resources") {
			t.Fatalf("Hub route %s still renders the selected external-resources section", target)
		}
		head := fetch("HEAD", target, "Mozilla/5.0")
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
			t.Fatalf("HEAD Hub route %s returned %d body=%d", target, head.Code, head.Body.Len())
		}
	}
	for _, target := range []string{"/apps/blink", "/apps/linkedin", "/linkage", "/links"} {
		if got := fetch("GET", target, "Mozilla/5.0"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"@type":"CollectionPage"`) {
			t.Errorf("configured trigger path %s returned %d without Hub markup", target, got.Code)
		}
	}
	if got := fetch("GET", "/apps/ordinary", "Mozilla/5.0"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `data-core-page-type=`) {
		t.Fatalf("ordinary detail path returned %d without legacy detail markup", got.Code)
	}
	detail := fetch("GET", "/apps/example?lan=id", "Mozilla/5.0")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), ">LK</a>") || !strings.Contains(detail.Body.String(), `href="/link/"`) {
		t.Fatal("detail page is missing the /link footer entry")
	}
	if queries != 0 {
		t.Fatalf("public requests performed %d SQL reads", queries)
	}
	conn, err := rt.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if got := fetch("GET", "/link/?lan=id", "Googlebot"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"@type":"CollectionPage"`) {
		t.Fatal("Hub page did not survive DB outage with the loaded snapshot")
	}
	if got := fetch("GET", "/apps/example?lan=id", "Googlebot"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `data-core-page-type=`) {
		t.Fatal("detail page did not survive DB outage with the loaded snapshot")
	}
}

func TestHubPathsWithPrefix(t *testing.T) {
	rt := testRuntime(t)
	if err := UpdateRuntime(t.Context(), rt.db, func(tx *gorm.DB) error {
		return tx.Model(&SiteConfig{}).Where("site_key = ?", "default").Update("route_prefix", "test").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.ReloadIfChanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithRuntime(rt, RouterOptions{AssetRoot: "../../assets"})
	for _, path := range []string{"/testlink", "/testlink/20261125/0/", "/apps/example?path=/testlink/"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "example.test"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"@type":"CollectionPage"`) {
			t.Errorf("prefixed Hub route %s returned %d without CollectionPage", path, w.Code)
		}
	}
}
