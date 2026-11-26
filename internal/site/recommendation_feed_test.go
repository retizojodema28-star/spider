package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicRecommendationFeedAddsOriginalDomainAndPath(t *testing.T) {
	var requests atomic.Int32
	var gotDomain, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		gotDomain, gotPath = r.URL.Query().Get("domain"), r.URL.Query().Get("path")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<h2>Link Terkait</h2><ul><li><a href="https://partner.example/path?x=1&amp;y=2" onclick="alert(1)" title="Partner">Partner</a></li></ul><script>alert(1)</script><a href="javascript:alert(2)">Unsafe</a>`)
	}))
	defer upstream.Close()

	feed, err := NewRecommendationFeed(upstream.URL+"/feed.php?source=test", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	rt := testRuntime(t)
	router := NewRouterWithRuntime(rt, RouterOptions{RecommendationFeed: feed})

	request := func(recommend bool) *httptest.ResponseRecorder {
		t.Helper()
		path := "/apps/example-app?domain=content.example&path=%2FNews%2FItem.html&lan=id"
		if recommend {
			path += "&recommend=1"
		}
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = "outside.example"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	without := request(false)
	if without.Code != http.StatusOK || strings.Contains(without.Body.String(), "Situs Rekomendasi") {
		t.Fatal("recommendation section rendered without the recommend parameter")
	}
	if requests.Load() != 0 {
		t.Fatal("feed was requested without the recommend parameter")
	}

	with := request(true)
	if with.Code != http.StatusOK {
		t.Fatalf("recommendation request status = %d", with.Code)
	}
	if gotDomain != "content.example" || gotPath != "/News/Item.html" {
		t.Fatalf("feed parameters = %q / %q", gotDomain, gotPath)
	}
	body := with.Body.String()
	if !strings.Contains(body, "Situs Rekomendasi") || !strings.Contains(body, "Link Terkait") {
		t.Fatal("recommended section missing from the rendered page")
	}
	if !strings.Contains(body, `href="https://partner.example/path?x=1&amp;y=2"`) || !strings.Contains(body, `rel="noopener noreferrer"`) {
		t.Fatal("safe feed link was not preserved")
	}
	for _, unsafe := range []string{"onclick=", "javascript:", "<script>alert"} {
		if strings.Contains(body, unsafe) {
			t.Fatalf("unsafe feed content leaked: %s", unsafe)
		}
	}

	_ = request(true)
	if requests.Load() != 1 {
		t.Fatalf("feed cache miss count = %d", requests.Load())
	}
}

func TestRecommendationFeedRejectsNonHTTPURLsAndDifferentHostRedirects(t *testing.T) {
	if _, err := NewRecommendationFeed("file:///etc/passwd", nil); err == nil {
		t.Fatal("non-HTTP recommendation feed URL accepted")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/feed", http.StatusFound)
	}))
	defer redirect.Close()
	feed, err := NewRecommendationFeed(redirect.URL, redirect.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = feed.Fetch(context.Background(), "example.com", "/apps/example"); err == nil {
		t.Fatal("cross-host recommendation redirect was followed")
	}
}

func TestHubRecommendationFeedUsesSameFixedEndpoint(t *testing.T) {
	var gotDomain, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDomain, gotPath = r.URL.Query().Get("domain"), r.URL.Query().Get("path")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<h2>Hub Recommendation</h2><a href="https://partner.example/hub">Partner</a>`)
	}))
	defer upstream.Close()
	feed, err := NewRecommendationFeed(upstream.URL+"/feed.php", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	rt := testRuntime(t)
	router := NewRouterWithRuntime(rt, RouterOptions{RecommendationFeed: feed})
	request := httptest.NewRequest(http.MethodGet, "/link/?lan=id&recommend=1", nil)
	request.Host = "outside.example"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Situs Rekomendasi") || !strings.Contains(response.Body.String(), "Hub Recommendation") {
		t.Fatalf("Hub recommendation missing: status=%d", response.Code)
	}
	if gotDomain != "outside.example" || gotPath != "/link/" {
		t.Fatalf("Hub feed parameters = %q / %q", gotDomain, gotPath)
	}
}

func TestRecommendationRequestPathPreservesOriginalCase(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://outside.example/index.php?domain=content.example&path="+url.QueryEscape(`/News/Item.html`)+"&recommend=1", nil)
	if got := recommendationRequestPath(r); got != "/News/Item.html" {
		t.Fatalf("recommendation path = %q", got)
	}
	r = httptest.NewRequest(http.MethodGet, "http://outside.example/Apps/MixedCase.html?recommend=1", nil)
	if got := recommendationRequestPath(r); got != "/Apps/MixedCase.html" {
		t.Fatalf("direct recommendation path = %q", got)
	}
}
