package site

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestDetailPageKeepsOriginalDownloadAnchor(t *testing.T) {
	renderer := pageRenderer{templates: template.Must(template.New("pages").Funcs(template.FuncMap{
		"requestImage": func(path string) string { return "/assets/" + strings.TrimPrefix(path, "/") },
		"json": func(value any) template.JS {
			data, _ := json.Marshal(value)
			return template.JS(data)
		},
	}).ParseFS(templateFiles, "templates/*.html"))}
	app := DetailPage{Config: SiteConfig{DisplayName: "APK Hub"}, Canonical: "https://example.test/apps/sample-app", LegacyBaseURL: "http://example.test", App: App{
		Slug: "sample-app", Name: "Sample App", PackageName: "example.sample", Developer: "Example",
		Version: "1.0", Category: "Puzzle", Language: "id", Description: "Contoh deskripsi.",
		SourceURL: "https://origin.invalid/sample-app/example.sample", CapturedAt: time.Now(),
		IconPath:    "samples/sample.png",
		Screenshots: []AppScreenshot{{LocalPath: "samples/sample-screen.jpg"}},
	}}
	var output bytes.Buffer
	if err := renderer.templates.ExecuteTemplate(&output, "detail.html", app); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if got := strings.Count(html, `href="#apk-info"`); got != 4 {
		t.Fatalf("expected developer and three download/info links to target #apk-info, got %d links", got)
	}
	if strings.Contains(html, "origin.invalid") {
		t.Fatal("source URL must not be exposed as a public page link")
	}
}

func TestDetailPageRendersExternalLinksOnlyAndNoSourceURL(t *testing.T) {
	renderer := pageRenderer{templates: template.Must(template.New("pages").Funcs(template.FuncMap{
		"requestImage": func(path string) string { return "/assets/" + strings.TrimPrefix(path, "/") },
		"json": func(value any) template.JS {
			data, _ := json.Marshal(value)
			return template.JS(data)
		},
	}).ParseFS(templateFiles, "templates/*.html"))}
	data := DetailPage{
		Config:        SiteConfig{DisplayName: "APK Hub"},
		Canonical:     "https://example.test/apps/sample-app",
		LegacyBaseURL: "http://example.test",
		KeywordURL:    "/apps/sample-keyword",
		KeywordChips:  []string{"sample keyword APK"},
		App:           App{Slug: "sample-app", Name: "Sample App", PackageName: "example.sample", Developer: "Example", Version: "1.0", Category: "Puzzle", Language: "id", Description: "Contoh deskripsi.", Rating: "9.0", Downloads: "1 juta+", IconPath: "samples/sample.png", SourceURL: "https://origin.invalid/item"},
		InternalLinks: []SiteLink{{AnchorText: "Game rekomendasi", TargetURL: "/apps/other", Category: "Rekomendasi"}},
		ExternalLinks: []SiteLink{{AnchorText: "Situs resmi", TargetURL: "https://publisher.example/", Category: "Pengembang"}},
	}
	var output bytes.Buffer
	if err := renderer.templates.ExecuteTemplate(&output, "detail.html", data); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, value := range []string{"Situs resmi", `href="https://publisher.example/"`, `rel="noopener"`, "canonical", "application/ld+json", "Pertanyaan umum", `href="/category/game/"`, `href="/category/adventure/"`, `href="/apps/sample-keyword">sample keyword APK`} {
		if !strings.Contains(html, value) {
			t.Errorf("detail page is missing %q", value)
		}
	}
	if strings.Contains(html, "Game rekomendasi") || strings.Contains(html, `href="/apps/other"`) {
		t.Fatal("internal links must not be shown in the related-links section")
	}
	if strings.Contains(html, `rel="nofollow`) {
		t.Fatal("external links must remain ordinary crawlable destinations")
	}
	if strings.Contains(html, "origin.invalid") {
		t.Fatal("source URL must not be exposed on the public page")
	}
}
