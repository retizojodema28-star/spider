package site

import (
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestHubGeneratedDateRoutesResolve(t *testing.T) {
	config := SiteConfig{DefaultLanguage: "id"}
	legacy := LegacySnapshot{Now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	for _, limit := range []int{10, 40, 2000, 9999} {
		cfg := DefaultHubConfig()
		cfg.DateNavNumMax = limit
		s := HubSnapshot{Config: &cfg}
		r := httptest.NewRequest("GET", "http://hub.example/link?lan=id", nil)
		r.RequestURI = "/link?lan=id"
		page, err := BuildHubPage(r, config, legacy, s)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range page.DateNavigation {
			if len(group.Links) != 10 {
				t.Fatalf("expected ten links for %s", group.Date)
			}
			for _, link := range group.Links {
				r := httptest.NewRequest("GET", link.URL, nil)
				r.RequestURI = r.URL.RequestURI()
				resolved, err := BuildHubPage(r, config, legacy, s)
				if err != nil {
					t.Fatal(err)
				}
				if resolved.RequestedDate != group.Date || resolved.NavigationDate != group.Date {
					t.Fatalf("generated route %s resolved to %s", link.URL, resolved.RequestedDate)
				}
			}
		}
	}
}

func TestHubDSLAndDefaults(t *testing.T) {
	if !ShouldHandleHub("/link", nil) || !ShouldHandleHub("/BLINKING", nil) || ShouldHandleHub("/apps", nil) {
		t.Fatal("default trigger is not substring routing")
	}
	for _, raw := range []string{"", `broken`, `null`, `[]`, `{"enabled":null}`, `{"enabled":true} garbage`} {
		if got := DecodeHubConfig([]byte(raw)); got != DefaultHubConfig() {
			t.Errorf("default decode %q: %#v", raw, got)
		}
	}
	for _, dsl := range []string{"{s0}", "{s33}", "{x2}", "s2", "{n999999999999999999999}"} {
		if len(ParseHubDSL(dsl)) != 0 {
			t.Errorf("invalid DSL %q accepted", dsl)
		}
	}
	if got := ParseHubDSL("{s2n3}{s1}"); !reflect.DeepEqual(got, []HubDSLSegment{{"s", 2}, {"n", 3}, {"s", 1}}) {
		t.Fatal(got)
	}
	prefixes, err := GenerateHubSubdomains("{s2n3}", 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range prefixes {
		if len(p) != 5 || seen[p] {
			t.Fatal(prefixes)
		}
		seen[p] = true
	}
	if _, err = GenerateHubSubdomains("{n1}", 11); err == nil {
		t.Fatal("expected namespace exhaustion")
	}
	if _, err = BuildHubPage(nil, SiteConfig{}, LegacySnapshot{}, HubSnapshot{}); err == nil {
		t.Fatal("nil request accepted")
	}
}

func TestHubLinkSelectionFiltersRecommendTierWithinPHPCount(t *testing.T) {
	rows := make([]LegacyManagedLink, 0, 100)
	for i := 0; i < 100; i++ {
		rows = append(rows, LegacyManagedLink{ID: uint(i + 1), Enabled: true, TargetURL: fmt.Sprintf("https://hub-%03d.example/", i+1), AnchorText: fmt.Sprintf("Hub %03d", i+1), RecommendTier: i % 3, Weight: 100})
	}
	cfg := DefaultHubConfig()
	cfg.ExternalMin = 10
	cfg.ExternalMax = 10
	cfg.BottomLinkCount = 2
	left, bottom, _, poolSize := hubSelectLinks("hub-test", "2026-11-25", "2026-11-25", cfg, LegacySnapshot{ExternalLinks: rows}, HubSnapshot{}, 0, false)
	if poolSize != 100 {
		t.Fatalf("pool size = %d", poolSize)
	}
	if len(left) != 10 || len(bottom) != 2 {
		t.Fatalf("full selection = %d + bottom %d, want 10 + 2", len(left), len(bottom))
	}
	seen := map[string]bool{}
	for _, link := range append(append([]HubLink{}, left...), bottom...) {
		if seen[link.URL] {
			t.Fatalf("duplicate hub link: %s", link.URL)
		}
		seen[link.URL] = true
	}
	left, bottom, _, poolSize = hubSelectLinks("hub-test", "2026-11-25", "2026-11-25", cfg, LegacySnapshot{ExternalLinks: rows}, HubSnapshot{}, 1, true)
	if poolSize != 33 || len(left) != 10 || len(bottom) != 2 {
		t.Fatalf("tier filter = pool %d, links %d + bottom %d", poolSize, len(left), len(bottom))
	}
	for _, link := range append(append([]HubLink{}, left...), bottom...) {
		if link.RecommendTier != 1 {
			t.Fatalf("tier filter leaked tier %d", link.RecommendTier)
		}
	}
	left, bottom, _, poolSize = hubSelectLinks("hub-test", "2026-11-25", "2026-11-25", cfg, LegacySnapshot{ExternalLinks: rows}, HubSnapshot{}, 2, true)
	if poolSize != 33 || len(left) != 10 || len(bottom) != 2 {
		t.Fatalf("tier two filter = pool %d, links %d + bottom %d", poolSize, len(left), len(bottom))
	}
	for _, link := range append(append([]HubLink{}, left...), bottom...) {
		if link.RecommendTier != 2 {
			t.Fatalf("tier two filter leaked tier %d", link.RecommendTier)
		}
	}
}

// ExampleBuildHubPage covers the pure builder boundary used by the public
// /link endpoint and the administration/migration code that owns its data.
func ExampleBuildHubPage() {
	r := httptest.NewRequest("GET", "http://example.test/link?lan=id", nil)
	r.RequestURI = "/link?lan=id"
	p, err := BuildHubPage(r, SiteConfig{DefaultLanguage: "id"}, LegacySnapshot{Now: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)}, HubSnapshot{})
	if errors.Is(err, ErrNotHubRoute) {
		return
	}
	if err != nil {
		panic(err)
	}
	_ = RenderHubPage(io.Discard, p)
	fmt.Println(p.StatusCode, p.Language, len(p.DateNavigation))
	// Output: 200 id 7
}
