package site

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestHubImagesUseExistingSingleImageBinding(t *testing.T) {
	root := t.TempDir()
	fixture, err := os.ReadFile("../../assets/samples/sample-app-one-icon.jpg")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bound/demo/1.jpg", "site/demo/1.jpg", "samples/fallback-icon.jpg", "samples/fallback-shot.jpg"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := IndexLegacyLocalImageFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	config := SiteConfig{DefaultLanguage: "id", ImageBasePath: "/assets/site"}
	legacy := LegacySnapshot{
		LanguageBank:    &LanguageBank{languages: map[string]languageData{"id": {words: []string{"demo"}}}},
		LocalImageFiles: files,
		Apps:            []App{{Slug: "fixture", Active: true, IconPath: "samples/fallback-icon.jpg", Screenshots: []AppScreenshot{{LocalPath: "samples/fallback-shot.jpg"}}}},
		ExternalLinks:   []LegacyManagedLink{{ID: 1, Enabled: true, TargetURL: "https://partner.example/path", AnchorText: "Partner"}},
		Now:             time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
	}
	r := httptest.NewRequest("GET", "/link/20261125/0/?lan=id", nil)
	r.Host = "hub.example.test"
	build := func(snapshot LegacySnapshot) HubPage {
		t.Helper()
		p, err := BuildHubPage(r, config, snapshot, HubSnapshot{ImageBaseURL: "https://ignored-remote.example/images"})
		if err != nil {
			t.Fatal(err)
		}
		knownFiles := snapshot.LocalImageFiles
		if knownFiles == nil {
			// Legacy compatibility callers may omit the index while passing
			// existing sample records. These fixture files were created above.
			knownFiles = files
		}
		assertHubLocalImageOutput(t, p, knownFiles)
		return p
	}
	p := build(legacy)
	if p.Images.Primary != "/assets/site/demo/1.jpg" || len(p.Images.Screenshots) != 1 {
		t.Fatalf("site image base not respected: %#v", p.Images)
	}
	legacy.ImageBindings = []DomainImageBinding{{Domain: "hub.example.test", ImageBasePath: "/assets/bound"}}
	bound := build(legacy)
	if bound.Images.Primary != "/assets/bound/demo/1.jpg" || !reflect.DeepEqual(bound.Images.Screenshots, []string{"/assets/bound/demo/1.jpg"}) {
		t.Fatalf("single-image binding invented files: %#v", bound.Images)
	}
	if len(bound.ResourceCards) != 1 || bound.ResourceCards[0].Image != bound.Images.Primary {
		t.Fatal("resource card did not reuse the sole indexed bound image")
	}
	legacy.ImageBindings[0].ImageBasePath = "/assets/missing"
	fallback := build(legacy)
	if fallback.Images.Primary != "/assets/samples/fallback-icon.jpg" {
		t.Fatalf("sample fixture missing or incorrectly rebased: %#v", fallback.Images)
	}
	legacy.ImageBindings[0].ImageBasePath = "https://remote.invalid/images"
	build(legacy)
	legacy.LocalImageFiles = map[string]bool{}
	empty := build(legacy)
	if empty.Images.Primary != "" || len(empty.Images.Screenshots) != 0 || empty.ResourceCards[0].Image != "" {
		t.Fatalf("missing assets produced invented images: %#v", empty.Images)
	}
	legacy.LocalImageFiles = nil
	if withoutIndex := build(legacy); withoutIndex.Images.Primary != "/assets/samples/fallback-icon.jpg" {
		t.Fatal("compatibility caller lost its supplied existing sample fixture")
	}
	if bound.Title != p.Title || fallback.Hash != p.Hash || !reflect.DeepEqual(bound.LeftLinks, p.LeftLinks) {
		t.Fatal("image selection changed public content or link selection")
	}
}

func TestHubFreshRouteUsesActualSampleImages(t *testing.T) {
	files, err := IndexLegacyLocalImageFiles("../../assets")
	if err != nil {
		t.Fatal(err)
	}
	legacy := LegacySnapshot{LocalImageFiles: files, Apps: []App{{Slug: "sample-app-one", Active: true, IconPath: "samples/sample-app-one-icon.jpg", Screenshots: []AppScreenshot{{LocalPath: "samples/sample-app-one-shot-1.jpg"}}}}}
	r := httptest.NewRequest("GET", "/link/20261125/0/?lan=id", nil)
	r.Host = "hub.example.test"
	p, err := BuildHubPage(r, SiteConfig{DefaultLanguage: "id", ImageBasePath: "/assets"}, legacy, HubSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Images.Primary != "/assets/samples/sample-app-one-icon.jpg" {
		t.Fatalf("fresh hub must use real local sample fallback: %#v", p.Images)
	}
	assertHubLocalImageOutput(t, p, files)
}

func assertHubLocalImageOutput(t *testing.T, page HubPage, files map[string]bool) {
	t.Helper()
	assert := func(value string) {
		t.Helper()
		if !strings.HasPrefix(value, "/assets/") || !legacyLocalPath(value) || !files[value] {
			t.Errorf("nonlocal or unindexed image emitted: %q", value)
		}
	}
	if page.Images.Primary != "" {
		assert(page.Images.Primary)
	}
	for _, value := range page.Images.Screenshots {
		assert(value)
	}
	for _, card := range page.ResourceCards {
		if card.Image != "" {
			assert(card.Image)
		}
	}
	var jsonImages func(any)
	jsonImages = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "image" {
					switch image := item.(type) {
					case string:
						assert(image)
					case []any:
						for _, image := range image {
							assert(image.(string))
						}
					}
				} else {
					jsonImages(item)
				}
			}
		case []any:
			for _, item := range v {
				jsonImages(item)
			}
		}
	}
	for _, script := range page.JSONLD {
		var v any
		if err := json.Unmarshal([]byte(script), &v); err != nil {
			t.Fatal(err)
		}
		jsonImages(v)
	}
	var body bytes.Buffer
	if err := RenderHubPage(&body, page); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(&body)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		attrs := map[string]string{}
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		if n.Type == html.ElementNode {
			if n.Data == "img" {
				assert(attrs["src"])
			}
			if n.Data == "meta" && (attrs["property"] == "og:image" || attrs["property"] == "og:image:secure_url" || attrs["name"] == "twitter:image") {
				assert(attrs["content"])
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}
