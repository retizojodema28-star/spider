package site

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLegacyRuntimeCollectionsSelectSiteOnly(t *testing.T) {
	php := `{"anchor_text":"Formal external","target_url":"https://external.example/","enabled":true}`
	probeReq := httptest.NewRequest("GET", "http://preview.example.test:18443/apps/sample-app-one?lan=id", nil)
	probeReq.RequestURI = "/apps/sample-app-one?lan=id"
	probe, err := BuildLegacyPage(probeReq, SiteConfig{DefaultLanguage: "id"}, LegacySnapshot{LanguageBank: legacyTestBank(t), Apps: legacyTestApps()})
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{
		Bank: legacyTestBank(t), Apps: legacyTestApps(), LocalImageFiles: map[string]bool{"/assets/custom/" + slugifyPHP(probe.Brand) + "/1.jpg": true},
		Links: []SiteLink{
			{ID: 1, SiteKey: "preview", Kind: "external", Enabled: true, TargetURL: "https://preview.example/"},
			{ID: 2, SiteKey: "default", Kind: "external", Domain: "unrelated.example", AppSlug: "other-app", Language: "th", Enabled: true, AnchorText: "Formal external", TargetURL: "https://external.example/"},
			{ID: 3, SiteKey: "default", Kind: "main-external", Language: "id", Enabled: true, TargetURL: "https://main.example/"},
			{ID: 4, SiteKey: "preview", Kind: "main-external", Enabled: true, TargetURL: "https://preview-main.example/"},
		},
		InternalDomains: []InternalDomain{
			{ID: 1, SiteKey: "preview", Domain: "preview.example", Enabled: true},
			{ID: 2, SiteKey: "default", Domain: "other.example", Language: "th", Path: "/apps", Enabled: true},
		},
		LinkMetadata: map[uint]RuntimeLinkMetadata{2: {LinkID: 2, PHPJSON: php}},
	}
	got := LegacySnapshotFromRuntime(s)
	if len(got.Links) != 2 || got.Links[0].ID != 2 || len(got.ExternalLinks) != 1 || len(got.MainExternalLinks) != 1 || len(got.InternalDomains) != 1 || got.InternalDomains[0].ID != 2 {
		t.Fatalf("wrong site collection: %#v", got)
	}
	if string(got.ExternalLinks[0].PHPJSON) != php || !reflect.DeepEqual(got.LocalImageFiles, s.LocalImageFiles) {
		t.Fatal("adapter dropped PHP ordering metadata or indexed image files")
	}
	if !reflect.DeepEqual(got, LegacySnapshotFromRuntimeForSite(s, "")) {
		t.Fatal("blank site selector did not select default")
	}
	got.ShowExternalPanel, got.ShowSubdomainPanel = true, true
	r := httptest.NewRequest("GET", "http://request.example/apps/sample-app-one?lan=id", nil)
	p, err := BuildLegacyPage(r, SiteConfig{SiteKey: "default", DefaultLanguage: "id", ExternalLinkLimit: 12}, got)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ExternalLinks) != 1 || p.ExternalLinks[0].Path != "https://external.example/" || len(p.SubdomainLinks) != 1 || p.SubdomainLinks[0].Path != "//other.example/apps" {
		t.Fatalf("request domain/app/language filtered PHP pools: %#v / %#v", p.ExternalLinks, p.SubdomainLinks)
	}
	preview := LegacySnapshotFromRuntimeForSite(s, "preview")
	if len(preview.Links) != 2 || preview.Links[0].ID != 1 || len(preview.InternalDomains) != 1 || preview.InternalDomains[0].ID != 1 {
		t.Fatal("explicit preview did not receive its own collections")
	}
	missing := LegacySnapshotFromRuntimeForSite(s, "missing")
	if len(missing.Links)+len(missing.InternalDomains)+len(missing.ExternalLinks)+len(missing.MainExternalLinks) != 0 {
		t.Fatal("unknown site leaked another site's collection")
	}
}

func TestLegacyRuntimePreviewSeedsDoNotChangeFormalHub(t *testing.T) {
	s := &Snapshot{Bank: legacyTestBank(t), Apps: legacyTestApps()}
	r := httptest.NewRequest("GET", "http://preview.example.test:18443/link/?lan=id", nil)
	cfg := SiteConfig{SiteKey: "default", DefaultLanguage: "id"}
	baseline, err := BuildHubPage(r, cfg, LegacySnapshotFromRuntime(s), HubSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	s.Links = []SiteLink{{ID: 1, SiteKey: "preview", Kind: "external", AnchorText: "Preview only", TargetURL: "https://preview.example/", Enabled: true}}
	s.InternalDomains = []InternalDomain{{ID: 1, SiteKey: "preview", Domain: "preview.example", Enabled: true}}
	formal, err := BuildHubPage(r, cfg, LegacySnapshotFromRuntime(s), HubSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, formal) {
		t.Fatal("preview seed collections changed formal hub")
	}
}

func TestLegacyRuntimeAdapterUsesPHPIDsWithoutChangingSQLHandles(t *testing.T) {
	s := &Snapshot{
		Links: []SiteLink{
			{ID: 100, SiteKey: "default", Kind: "external", Enabled: true, Weight: 100, TargetURL: "https://first.example/"},
			{ID: 200, SiteKey: "default", Kind: "external", Enabled: true, Weight: 100, TargetURL: "https://second.example/"},
			{ID: 300, SiteKey: "default", Kind: "main-external"},
			{ID: 400, SiteKey: "default", Kind: "main-external"},
		},
		LinkMetadata: map[uint]RuntimeLinkMetadata{
			100: {LinkID: 100, PHPID: 2, PHPJSON: `{"id":2}`},
			200: {LinkID: 200, PHPID: 1, PHPJSON: `{"id":1}`},
			300: {LinkID: 300, PHPID: 7, PHPJSON: `{"id":7}`},
			400: {LinkID: 400}, // old fixture: explicit zero falls back to SQL ID
		},
		InternalDomains: []InternalDomain{
			{ID: 500, PHPID: 3, SiteKey: "default"},
			{ID: 600, SiteKey: "default"},
		},
	}
	got := LegacySnapshotFromRuntime(s)
	if got.ExternalLinks[0].ID != 2 || got.ExternalLinks[1].ID != 1 || got.MainExternalLinks[0].ID != 7 || got.MainExternalLinks[1].ID != 400 || got.InternalDomains[0].ID != 3 || got.InternalDomains[1].ID != 600 {
		t.Fatal("adapter lost logical IDs or old-fixture fallback")
	}
	if string(got.ExternalLinks[0].PHPJSON) != `{"id":2}` || got.Links[0].ID != 100 || s.Links[0].ID != 100 || s.InternalDomains[0].ID != 500 {
		t.Fatal("adapter changed serialized PHP row or SQL admin handles")
	}
	if top := legacyExternalRows(got.ExternalLinks, 1); len(top) != 1 || top[0].TargetURL != "https://first.example/" {
		t.Fatal("equal-weight tie selection used SQL IDs instead of PHP IDs")
	}
}
