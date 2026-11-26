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

func TestLegacyRecommendSelectsAllOrMatchingTier(t *testing.T) {
	external := make([]LegacyManagedLink, 0, 36)
	for i := 0; i < 36; i++ {
		external = append(external, LegacyManagedLink{
			ID:            uint(i + 1),
			Enabled:       true,
			AnchorText:    fmt.Sprintf("External %02d", i+1),
			TargetURL:     fmt.Sprintf("https://tier-%d-%02d.example/", i%3, i+1),
			RecommendTier: i % 3,
		})
	}
	snapshot := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), ShowExternalPanel: true, ExternalLinks: external}
	build := func(target string) LegacyPage {
		t.Helper()
		request := httptest.NewRequest("GET", target, nil)
		request.RequestURI = strings.TrimPrefix(target, "http://preview.example.test")
		page, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 200}, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	all := build("http://preview.example.test/apps/sample-app-one?lan=id&recommend=0")
	defaultSelection := build("http://preview.example.test/apps/sample-app-one?lan=id")
	tierOne := build("http://preview.example.test/apps/sample-app-one?lan=id&recommend=1")
	tierTwo := build("http://preview.example.test/apps/sample-app-one?lan=id&recommend=2")
	feedOnly := build("http://preview.example.test/apps/sample-app-one?lan=id&feed=1")
	bare := build("http://preview.example.test/apps/sample-app-one")
	bareZero := build("http://preview.example.test/apps/sample-app-one?recommend=0")
	bareFeed := build("http://preview.example.test/apps/sample-app-one?feed=1")
	if len(all.ExternalLinks) != 36 || len(tierOne.ExternalLinks) != 12 || len(tierTwo.ExternalLinks) != 12 {
		t.Fatalf("filtered links = all %d, tier1 %d, tier2 %d", len(all.ExternalLinks), len(tierOne.ExternalLinks), len(tierTwo.ExternalLinks))
	}
	if all.Hash != tierOne.Hash || all.Hash != tierTwo.Hash || all.Hash != feedOnly.Hash || all.Hash != defaultSelection.Hash {
		t.Fatal("recommend and feed must not change the page hash")
	}
	if bare.Hash != bareZero.Hash || bare.Hash != bareFeed.Hash {
		t.Fatal("recommend=0 or feed=1 changed a query-free page hash")
	}
	paths := func(page LegacyPage) []string {
		out := make([]string, 0, len(page.ExternalLinks))
		for _, link := range page.ExternalLinks {
			out = append(out, link.Path)
		}
		return out
	}
	if strings.Join(paths(all), "\n") != strings.Join(paths(defaultSelection), "\n") || strings.Join(paths(all), "\n") != strings.Join(paths(feedOnly), "\n") {
		t.Fatal("recommend=0 and feed=1 must retain the full external-link selection")
	}
	seenTiers := map[string]bool{}
	for _, link := range all.ExternalLinks {
		if strings.Contains(link.Path, "tier-0-") {
			seenTiers["0"] = true
		}
		if strings.Contains(link.Path, "tier-1-") {
			seenTiers["1"] = true
		}
		if strings.Contains(link.Path, "tier-2-") {
			seenTiers["2"] = true
		}
	}
	if !seenTiers["0"] || !seenTiers["1"] || !seenTiers["2"] {
		t.Fatal("recommend=0 must include the complete recommend_tier pool")
	}
	for _, link := range tierOne.ExternalLinks {
		if !strings.Contains(link.Path, "tier-1-") {
			t.Fatalf("recommend=1 returned a non-tier-1 link: %s", link.Path)
		}
	}
	for _, link := range tierTwo.ExternalLinks {
		if !strings.Contains(link.Path, "tier-2-") {
			t.Fatalf("recommend=2 returned a non-tier-2 link: %s", link.Path)
		}
	}
}

func TestLegacyRecommendFiltersBeforeTwoHundredRowCeiling(t *testing.T) {
	external := make([]LegacyManagedLink, 0, 210)
	for i := 0; i < 205; i++ {
		external = append(external, LegacyManagedLink{ID: uint(i + 1), Enabled: true, AnchorText: fmt.Sprintf("Tier two %03d", i+1), TargetURL: fmt.Sprintf("https://tier-2-%03d.example/", i+1), RecommendTier: 2, Weight: 1000})
	}
	for i := 0; i < 5; i++ {
		external = append(external, LegacyManagedLink{ID: uint(206 + i), Enabled: true, AnchorText: fmt.Sprintf("Tier one %03d", i+1), TargetURL: fmt.Sprintf("https://tier-1-%03d.example/", i+1), RecommendTier: 1, Weight: 0})
	}
	snapshot := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), ShowExternalPanel: true, ExternalLinks: external}
	request := httptest.NewRequest("GET", "http://preview.example.test/apps/sample-app-one?lan=id&recommend=1", nil)
	request.RequestURI = "/apps/sample-app-one?lan=id&recommend=1"
	page, err := BuildLegacyPage(request, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.ExternalLinks) != 5 {
		t.Fatalf("tier 1 after 200-row ceiling = %d, want 5", len(page.ExternalLinks))
	}
	for _, link := range page.ExternalLinks {
		if !strings.Contains(link.Path, "tier-1-") {
			t.Fatalf("pre-limit filter leaked %s", link.Path)
		}
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
