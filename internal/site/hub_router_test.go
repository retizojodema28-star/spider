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
// The public Hub endpoint is retired, so all former Hub routes are 404 while
// ordinary detail pages remain available and no public SQL reads occur.
func TestPublicHubRoutesAreRemoved(t *testing.T) {
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
		"/%6cink/",
		"/link%2f20261125/0/",
		"/link?path=/apps/example",
		"/link/?__apk_home_fallback=1",
		"/?path=%252Flink%252F",
	} {
		for _, method := range []string{"GET", "HEAD"} {
			if got := fetch(method, target, "Mozilla/5.0"); got.Code != 404 {
				t.Fatalf("retired Hub route %s %s returned %d", method, target, got.Code)
			}
		}
	}
	for _, target := range []string{"/apps/blink", "/apps/linkedin", "/linkage", "/links"} {
		if got := fetch("GET", target, "Mozilla/5.0"); got.Code != 200 {
			t.Errorf("unrelated detail path %s returned %d", target, got.Code)
		}
	}
	detail := fetch("GET", "/apps/example?lan=id", "Mozilla/5.0")
	if detail.Code != 200 || strings.Contains(detail.Body.String(), ">LK<") || strings.Contains(detail.Body.String(), `href="/link/`) {
		t.Fatal("detail page still exposes the retired /link footer entry")
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
	if got := fetch("GET", "/apps/example?lan=id", "Googlebot"); got.Code != 200 || !strings.Contains(got.Body.String(), `data-core-page-type=`) {
		t.Fatal("detail page did not survive DB outage with the loaded snapshot")
	}
}

func TestRetiredLinkPathsWithPrefix(t *testing.T) {
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
	for _, path := range []string{"/testlink", "/testlink/20261125/0/", "/testrouter.php/link/", "/apps/example?path=/testlink/"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "example.test"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("retired prefixed route %s returned %d", path, w.Code)
		}
	}
}
