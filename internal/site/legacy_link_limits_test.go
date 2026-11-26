package site

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLegacyExternalLinksUseStableTwelveItemSet(t *testing.T) {
	external := make([]LegacyManagedLink, 0, 20)
	mainExternal := make([]LegacyManagedLink, 0, 20)
	for i := 0; i < 20; i++ {
		external = append(external, LegacyManagedLink{
			ID: uint(i + 1), Enabled: true, AnchorText: fmt.Sprintf("External %02d", i+1), TargetURL: fmt.Sprintf("https://external-%02d.example/", i+1),
		})
		mainExternal = append(mainExternal, LegacyManagedLink{
			ID: uint(i + 101), Enabled: true, Language: "id", AnchorText: fmt.Sprintf("Main %02d", i+1), TargetURL: fmt.Sprintf("https://main-%02d.example/", i+1),
		})
	}
	requestedRelated := 100
	dayOne := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	snapshot := LegacySnapshot{
		LanguageBank:      legacyTestBank(t),
		Apps:              legacyTestApps(),
		ShowExternalPanel: true,
		MaxRelatedLinks:   &requestedRelated,
		ExternalLinks:     external,
		MainExternalLinks: mainExternal,
		Now:               dayOne,
	}
	request := httptest.NewRequest("GET", "http://preview.example.test/apps/sample-app-one?lan=id", nil)
	request.RequestURI = "/apps/sample-app-one?lan=id"
	page, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(page.ExternalLinks), 12; got != want {
		t.Fatalf("ordinary external links = %d, want %d", got, want)
	}
	if got, want := len(page.MainExternalLinks), 12; got != want {
		t.Fatalf("main external links = %d, want %d", got, want)
	}
	if got := len(page.RelatedLinks); got > legacyMaxRelatedLinks {
		t.Fatalf("related links = %d, want at most %d", got, legacyMaxRelatedLinks)
	}
	html := legacyRenderForTest(t, page)
	if !strings.Contains(html, "Tautan eksternal") {
		t.Fatal("external section does not use the localized PHP-compatible heading")
	}
	if strings.Contains(html, "100 links") {
		t.Fatal("hard-coded English link count leaked into the page")
	}

	snapshot.Now = dayOne.AddDate(0, 0, 1)
	nextDay, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	firstSet := map[string]bool{}
	nextSet := map[string]bool{}
	for _, link := range page.ExternalLinks {
		firstSet[link.Path] = true
	}
	for _, link := range nextDay.ExternalLinks {
		nextSet[link.Path] = true
	}
	if len(firstSet) == 0 || !mapKeysEqual(firstSet, nextSet) {
		t.Fatal("external-link set changed for the same route on the next UTC day")
	}
}

func TestLegacyExternalLinksSplitTiersWithoutChangingPHPLimit(t *testing.T) {
	external := make([]LegacyManagedLink, 0, 24)
	for i := 0; i < 24; i++ {
		external = append(external, LegacyManagedLink{
			ID:            uint(i + 1),
			Enabled:       true,
			AnchorText:    fmt.Sprintf("External %02d", i+1),
			TargetURL:     fmt.Sprintf("https://tier-%02d.example/", i+1),
			RecommendTier: i % 2,
		})
	}
	snapshot := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), ShowExternalPanel: true, ExternalLinks: external}
	request := httptest.NewRequest("GET", "http://preview.example.test/apps/sample-app-one?lan=id", nil)
	request.RequestURI = "/apps/sample-app-one?lan=id"
	page, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.ExternalLinks) != 6 || len(page.RecommendedExternalLinks) != 6 {
		t.Fatalf("tiered links = %d + %d, want 6 + 6", len(page.ExternalLinks), len(page.RecommendedExternalLinks))
	}
	seen := map[string]bool{}
	for _, link := range append(append([]LegacyLink{}, page.ExternalLinks...), page.RecommendedExternalLinks...) {
		if seen[link.Path] {
			t.Fatalf("tier link duplicated: %s", link.Path)
		}
		seen[link.Path] = true
	}
}

func TestRecommendParameterDoesNotChangeExternalSelection(t *testing.T) {
	external := make([]LegacyManagedLink, 0, 40)
	for i := 0; i < 40; i++ {
		external = append(external, LegacyManagedLink{ID: uint(i + 1), Enabled: true, AnchorText: fmt.Sprintf("External %02d", i+1), TargetURL: fmt.Sprintf("https://hash-%02d.example/", i+1), RecommendTier: i % 2})
	}
	snapshot := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), ShowExternalPanel: true, ExternalLinks: external}
	build := func(target string) []string {
		t.Helper()
		request := httptest.NewRequest("GET", target, nil)
		request.RequestURI = strings.TrimPrefix(target, "http://preview.example.test")
		page, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, link := range append(append([]LegacyLink{}, page.ExternalLinks...), page.RecommendedExternalLinks...) {
			out = append(out, link.Path)
		}
		return out
	}
	without := build("http://preview.example.test/apps/sample-app-one?lan=id")
	with := build("http://preview.example.test/apps/sample-app-one?lan=id&recommend=1")
	if strings.Join(without, "\n") != strings.Join(with, "\n") {
		t.Fatal("recommend parameter changed tiered external-link selection")
	}
}

func mapKeysEqual(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if !right[key] {
			return false
		}
	}
	return true
}
