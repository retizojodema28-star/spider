package site

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testSiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "site.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPublicRouterSlashForms(t *testing.T) {
	db := testSiteDB(t)
	if err := Seed(db, "../../data/seed/apps.json"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANGUAGE_DATA_DIR", "../../data/lan")
	router := NewRouterWithLanguageBank(db, nil)
	for _, path := range []string{"/apps", "/category", "/categories", "/list", "/tag", "/tags", "/category/game", "/category/adventure", "/apps/sample-app-one", "/apps/missing", "/category/game/extra", "/some/deep/path"} {
		for _, slash := range []string{"", "/"} {
			uri := path + slash + "?lan=id"
			req := httptest.NewRequest("GET", uri, nil)
			req.Host = "preview.example.test:18443"
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `rel="canonical" href="https://preview.example.test`+path+`"`) {
				t.Errorf("GET %s: status=%d canonical or Location mismatch", uri, w.Code)
			}
		}
	}
	for _, tc := range [][2]string{{"POST", "/category/game/"}, {"GET", "/admin/api/unknown"}, {"GET", "/assets/missing.png"}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc[0], tc[1], nil))
		if w.Code != 404 {
			t.Errorf("%s %s: got %d, want 404", tc[0], tc[1], w.Code)
		}
	}
}

func TestIndonesianCategoryPHPResponses(t *testing.T) {
	db := testSiteDB(t)
	bank, err := LoadLanguageBank("../../data/lan", "id")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithLanguageBank(db, bank)
	for _, tc := range []struct{ host, path, title, kind string }{
		{"preview.example.test:18443", "/category/sample/?lan=id", "", "subdomain"},
		{"preview.example.test:18443", "/category/reader/?lan=id", "", "subdomain"},
		{"example.com", "/category/game/?lan=id", "", "detail"},
		{"example.com", "/apps", "", "list"},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Host = tc.host
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		html := w.Body.String()
		if w.Code != 200 || !strings.Contains(html, `data-core-page-type="`+tc.kind+`"`) || (tc.title != "" && !strings.Contains(html, "<title>"+tc.title+"</title>")) {
			t.Errorf("GET %s%s: expected %s %s", tc.host, tc.path, tc.title, tc.kind)
		}
	}
}

func TestRestartDoesNotRestoreEditedOrDeletedSampleData(t *testing.T) {
	db := testSiteDB(t)
	seed := "../../data/seed/apps.json"
	if err := Seed(db, seed); err != nil {
		t.Fatal(err)
	}
	var link SiteLink
	if err := db.First(&link).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&link).Updates(map[string]any{"enabled": false, "weight": 4321}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("slug = ?", "sample-app-one").Delete(&App{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, seed); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&link, link.ID).Error; err != nil || link.Enabled || link.Weight != 4321 {
		t.Fatalf("link not preserved: %v %v", link, err)
	}
	var n int64
	if err := db.Model(&App{}).Where("slug = ?", "sample-app-one").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("deleted app restored: count=%d err=%v", n, err)
	}
}
