package site

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLegacyImageBindingsUseExistingFilesAndFallBack(t *testing.T) {
	root := t.TempDir()
	cfg := SiteConfig{DefaultLanguage: "id", ImageBasePath: "/assets/missing", ExternalLinkLimit: 12}
	snapshot := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), ImageBindings: []DomainImageBinding{{Domain: "preview.example.test", ImageBasePath: "/assets/custom"}}}
	req := httptest.NewRequest("GET", "http://preview.example.test:18443/apps/sample-app-one?lan=id", nil)
	req.RequestURI = "/apps/sample-app-one?lan=id"
	probe, err := BuildLegacyPage(req, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// The generated brand is data-driven. Build the fixture directory from the
	// current language bank instead of coupling the test to a retired sample.
	imageSlug := slugifyPHP(probe.Brand)
	for _, file := range []string{
		filepath.Join("custom", imageSlug, "1.jpg"),
		filepath.Join("custom", imageSlug, "2.webp"),
		"samples/sample-app-one-icon.jpg",
		"samples/sample-app-one-shot-1.jpg",
		"samples/sample-app-one-shot-2.jpg",
	} {
		name := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("image fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := IndexLegacyLocalImageFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.LocalImageFiles = files
	p, err := BuildLegacyPage(req, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wantPrimary := "/assets/custom/" + imageSlug + "/1.jpg"
	if p.Images.Primary != wantPrimary || len(p.Images.Screenshots) != 2 {
		t.Fatalf("bound existing images not selected: %#v", p.Images)
	}
	for _, file := range p.Images.Screenshots {
		if !files[file] {
			t.Fatalf("invented image URL %s", file)
		}
	}
	snapshot.ImageBindings[0].ImageBasePath = "/assets/does-not-exist"
	fallback, err := BuildLegacyPage(req, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Images.Primary != "/assets/samples/sample-app-one-icon.jpg" {
		t.Fatalf("fixture was incorrectly rebased: %#v", fallback.Images)
	}
	if fallback.Title != p.Title || fallback.Hash != p.Hash || fallback.Version != p.Version {
		t.Fatal("images changed content")
	}
	snapshot.ImageBindings[0].ImageBasePath = "https://remote.invalid/images"
	local, err := BuildLegacyPage(req, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(local.Images.Primary, "remote.invalid") {
		t.Fatal("remote image accepted")
	}
}

func TestLegacySamplesAndGenericLinksDoNotOverridePublicPages(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/apps/sample-app-one?lan=id", nil)
	req.RequestURI = "/apps/sample-app-one?lan=id"
	cfg := SiteConfig{DefaultLanguage: "id", ExternalLinkLimit: 12}
	s := LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps(), Now: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)}
	p, err := BuildLegacyPage(req, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Links = []SiteLink{{Kind: "internal", Enabled: true, TargetURL: "/sample-only", AnchorText: "Sample"}, {Kind: "external", Enabled: true, TargetURL: "https://fixture.invalid", AnchorText: "Fixture", Language: "id"}}
	withSeeds, err := BuildLegacyPage(req, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	if legacyRenderForTest(t, p) != legacyRenderForTest(t, withSeeds) {
		t.Fatal("disabled panels/generic sample links polluted PHP output")
	}
	if p.Brand == s.Apps[0].Name || p.Version == s.Apps[0].Version || strings.Contains(p.Robots, "noindex") {
		t.Fatal("sample data overrode formal page")
	}
	s.Preview = true
	preview, err := BuildLegacyPage(req, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Robots != "noindex, nofollow" {
		t.Fatalf("preview indexable: %s", preview.Robots)
	}
}

func TestLegacyBindingLanguagePrecedenceAndFallback(t *testing.T) {
	bank := &LanguageBank{languages: map[string]languageData{"id": {words: []string{"id word"}}, "en": {words: []string{"en word"}}, "th": {sentences: []string{"a sentence is a valid language pack"}}}}
	req := httptest.NewRequest("GET", "http://entry.example:88/index.php?domain=content.example&path=%2Fapps%2Fx&lan=vn", nil)
	req.RequestURI = req.URL.RequestURI()
	cfg := SiteConfig{DefaultLanguage: "id"}
	s := LegacySnapshot{LanguageBank: bank, DomainBindings: []DomainBinding{{Domain: "content.example", Language: "th"}, {Domain: "entry.example", Language: "en"}}}
	p, err := BuildLegacyPage(req, cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	if p.RequestedLanguage != "th" || p.Language != "th" {
		t.Fatalf("content binding must beat entry/query: %s/%s", p.RequestedLanguage, p.Language)
	}
	s.DomainBindings = s.DomainBindings[1:]
	p, _ = BuildLegacyPage(req, cfg, s)
	if p.Language != "en" {
		t.Fatal("entry binding did not override query")
	}
	s.DomainBindings = nil
	p, _ = BuildLegacyPage(req, cfg, s)
	if p.RequestedLanguage != "vn" || p.Language != "en" {
		t.Fatal("missing pack must use English before site default")
	}
	req.RequestURI = "/apps/x?lan=unsupported"
	p, _ = BuildLegacyPage(req, cfg, s)
	if p.RequestedLanguage != "en" {
		t.Fatal("invalid explicit language must map to en")
	}
	req.RequestURI = "/apps/x?lan=&lang=en"
	p, _ = BuildLegacyPage(req, cfg, s)
	if p.RequestedLanguage != "id" {
		t.Fatal("present blank lan must shadow lang and use default")
	}
}

func TestLegacyPrefixRulesAndSingleDecode(t *testing.T) {
	if got := NormalizeLegacyPrefix(" A ? B "); got != "a--b" {
		t.Fatalf("PHP prefix replacement order changed: %q", got)
	}
	for _, test := range []struct{ path, prefix, apply, remove string }{
		{"/apps/item?x=1#top", "pre", "/preapps/item?x=1#top", "/apps/item?x=1#top"},
		{"/preapps/item", "pre", "/prepreapps/item", "/apps/item"},
		{"/link/", "pre", "/link/", "/link/"},
		{"/anything/myLink/path", "#", "/anything/myLink/path", "/anything/myLink/path"},
		{"/apps/item", "#", "/#", "/apps/item"},
		{"/", "#", "/#", "/"},
		{"https://example.com/apps", "pre", "https://example.com/apps", "https://example.com/apps"},
		{"/prehealth", "pre", "/preprehealth", "/prehealth"},
	} {
		if got := ApplyLegacyPrefix(test.path, test.prefix); got != test.apply {
			t.Errorf("apply %q: %q != %q", test.path, got, test.apply)
		}
		if got := RemoveLegacyPrefix(test.path, test.prefix); got != test.remove {
			t.Errorf("remove %q: %q != %q", test.path, got, test.remove)
		}
	}
	if got := normalizePHPPath("/%2563ategory"); got != "/%63ategory" {
		t.Fatalf("decoded twice: %q", got)
	}
	if got := normalizePHPHost("[::1]:8080"); got != "[::1]" {
		t.Fatalf("PHP preserves IPv6 brackets: %q", got)
	}
	if got := uniqueLines("École\nécole\nABC\nabc\n", true); !reflect.DeepEqual(got, []string{"École", "école", "ABC"}) {
		t.Fatalf("PHP strtolower is ASCII, not Unicode casefold: %#v", got)
	}
}
