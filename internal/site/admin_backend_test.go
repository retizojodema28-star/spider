package site

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const adminTestPassword = "H6!vR2@qZ9#kT8"

type adminHarness struct {
	t           *testing.T
	db          *gorm.DB
	rt          *Runtime
	router      *gin.Engine
	cookie      *http.Cookie
	csrf        string
	assets, lan string
}

func newAdminHarness(t *testing.T, db *gorm.DB) *adminHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if db == nil {
		var err error
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admin.sqlite")+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { sqlDB.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateAdmin(db); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("ADMIN_USERNAME", "Operator")
	t.Setenv("ADMIN_PASSWORD", adminTestPassword)
	t.Setenv("ADMIN_PASSWORD_HASH", "")
	t.Setenv("ADMIN_ORIGIN", "")
	if err := BootstrapAdminFromEnv(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assets, lan := t.TempDir(), t.TempDir()
	t.Setenv("ASSET_DIR", assets)
	for _, p := range []string{"samples", "variant/samples"} {
		if err := os.MkdirAll(filepath.Join(assets, p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(assets, p, "icon.png"), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(lan, "id"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lan, "id", "word.txt"), []byte("awal\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lan, "id", "sentences.txt"), []byte("Kalimat bahasa Indonesia yang cukup panjang untuk pengujian.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&SiteConfig{SiteKey: "default", DisplayName: "Test", DefaultLanguage: "id", DownloadBaseURL: "https://download.invalid/apk/", ImageBasePath: "/assets", ExternalLinkLimit: 12, MaxRelatedLinks: 6}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&App{Slug: "sample", Name: "Sample", PackageName: "test.sample", Language: "id", IconPath: "samples/icon.png", Active: true, CapturedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(context.Background(), db, RuntimeOptions{LanguageRoot: lan, AssetRoot: assets})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	if err := RegisterAdminRoutes(router, rt, AdminOptions{}); err != nil {
		t.Fatal(err)
	}
	return &adminHarness{t: t, db: db, rt: rt, router: router, assets: assets, lan: lan}
}

func (h *adminHarness) request(method, path string, body any, origin, csrf string) (*httptest.ResponseRecorder, map[string]any) {
	h.t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "http://preview.test"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if h.cookie != nil {
		r.AddCookie(h.cookie)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "apk_admin_session" {
			h.cookie = cookie
		}
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func (h *adminHarness) call(method, path string, body any, status int) map[string]any {
	h.t.Helper()
	w, out := h.request(method, path, body, "http://preview.test", h.csrf)
	if w.Code != status {
		h.t.Fatalf("%s %s: want %d got %d: %s", method, path, status, w.Code, w.Body.String())
	}
	return out
}

func (h *adminHarness) login() {
	h.t.Helper()
	session := h.call("GET", "/admin/api/session", nil, 200)
	h.csrf = session["data"].(map[string]any)["csrf_token"].(string)
	result := h.call("POST", "/admin/api/session/login", map[string]any{"username": "operator", "password": adminTestPassword}, 200)
	h.csrf = result["data"].(map[string]any)["csrf_token"].(string)
}

func TestAdminBootstrapSessionCSRFAndLogout(t *testing.T) {
	h := newAdminHarness(t, nil)
	var user AdminUser
	if err := h.db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash == adminTestPassword || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(adminTestPassword)) != nil {
		t.Fatal("bootstrap did not bcrypt hash password")
	}
	t.Setenv("ADMIN_PASSWORD", "F7#gA2!mP8@vQ5")
	if err := BootstrapAdminFromEnv(context.Background(), h.db); err != nil {
		t.Fatal(err)
	}
	var again AdminUser
	h.db.First(&again)
	if again.PasswordHash != user.PasswordHash {
		t.Fatal("restart overwrote password")
	}
	h.call("GET", "/admin/api/sites/config", nil, 401)
	s := h.call("GET", "/admin/api/session", nil, 200)
	h.csrf = s["data"].(map[string]any)["csrf_token"].(string)
	oldCookie := *h.cookie
	oldCSRF := h.csrf
	if h.cookie.Secure || !h.cookie.HttpOnly || h.cookie.SameSite != http.SameSiteLaxMode || h.cookie.Path != "/admin" {
		t.Fatalf("HTTP preview cookie flags: %+v", h.cookie)
	}
	for _, origin := range []string{"", "http://attacker.test", "null"} {
		w, _ := h.request("POST", "/admin/api/session/login", map[string]any{"username": "operator", "password": adminTestPassword}, origin, h.csrf)
		if w.Code != 403 {
			t.Fatalf("login origin %q accepted: %d", origin, w.Code)
		}
	}
	r := h.call("POST", "/admin/api/session/verify", map[string]any{"username": "operator", "password": adminTestPassword}, 200)
	h.csrf = r["data"].(map[string]any)["csrf_token"].(string)
	if oldCookie.Value == h.cookie.Value || oldCSRF == h.csrf {
		t.Fatal("login did not rotate session and CSRF")
	}
	var session AdminSession
	if err := h.db.Where("token_hash = ?", adminTokenHash(h.cookie.Value)).First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.TokenHash == h.cookie.Value {
		t.Fatal("raw session token stored")
	}
	for _, token := range []string{"", oldCSRF, "wrong"} {
		w, _ := h.request("POST", "/admin/api/sites/config", map[string]any{"site_name": "x"}, "http://preview.test", token)
		if w.Code != 403 {
			t.Fatalf("invalid CSRF accepted: %d", w.Code)
		}
	}
	authCookie := *h.cookie
	h.cookie = &oldCookie
	h.call("GET", "/admin/api/sites/config", nil, 401)
	h.cookie = &authCookie
	h.call("POST", "/admin/api/session/logout", map[string]any{}, 200)
	h.cookie = &authCookie
	h.call("GET", "/admin/api/sites/config", nil, 401)
}

func TestAdminSettingsBindingsAndLocalImages(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	for _, prefix := range []string{"test", "#", ""} {
		h.call("POST", "/admin/api/sites/config", map[string]any{"route_prefix": prefix, "show_list_panel": false, "max_related_links": 0, "main_page_external_link_limit": 0}, 200)
		s := h.rt.Snapshot()
		if s.Config.RoutePrefix != prefix || s.Config.MaxRelatedLinks != 0 || s.Config.ExternalLinkLimit != 0 {
			t.Fatalf("settings lost explicit zero: %+v", s.Config)
		}
	}
	h.call("POST", "/admin/api/sites/config", map[string]any{"max_related_links": 7}, 422)
	h.call("POST", "/admin/api/sites/config", map[string]any{"default_lang": "../../en"}, 422)
	h.call("POST", "/admin/api/sites/config", map[string]any{"site_key": "new", "site_name": "new"}, 422)
	for _, image := range []string{"https://cdn.test/", "/assets/missing", "/assets/../secret", "/assets/%2e%2e/secret"} {
		h.call("POST", "/admin/api/sites/config", map[string]any{"image_base_url": image}, 422)
	}
	h.call("POST", "/admin/api/sites/config", map[string]any{"image_base_url": "/assets/variant"}, 200)
	if !h.rt.Snapshot().LocalImageFiles["/assets/variant/samples/icon.png"] {
		t.Fatal("asset index missing reachable variant")
	}
	b := map[string]any{"domain": "EXAMPLE.com:8080", "lang": "id", "remark": "first"}
	h.call("POST", "/admin/api/domain-lang-bindings", b, 200)
	h.call("POST", "/admin/api/domain-lang-bindings", b, 409)
	b["remark"] = "updated"
	h.call("PUT", "/admin/api/domain-lang-bindings", b, 200)
	if got := h.rt.Snapshot().DomainBindings; len(got) != 1 || got[0].Domain != "example.com" || got[0].Remark != "updated" {
		t.Fatalf("binding not updated: %+v", got)
	}
	h.call("PUT", "/admin/api/domain-lang-bindings", map[string]any{"domain": "missing.test", "lang": "id"}, 404)
	h.call("POST", "/admin/api/domain-image-bindings", map[string]any{"domain": "example.com", "image_base_url": "/assets/variant"}, 200)
	if h.rt.Snapshot().ConfigForHost("example.com:8080").ImageBasePath != "/assets/variant" {
		t.Fatal("domain image binding not resolved")
	}
	h.call("DELETE", "/admin/api/domain-lang-bindings", map[string]any{"domains": []string{"example.com"}}, 200)
	h.call("DELETE", "/admin/api/domain-image-bindings", map[string]any{"domain": "example.com"}, 200)
}

func TestAdminLinksImportRollbackAndPreprocess(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	created := h.call("POST", "/admin/api/external-links", map[string]any{"anchor_text": "Original", "target_url": "https://example.com/", "recommend_tier": 1, "enabled": false, "weight": 0}, 200)
	id := created["data"].(map[string]any)["link"].(map[string]any)["id"]
	var link SiteLink
	h.db.First(&link)
	if link.Enabled || link.Weight != 0 || link.Language != "id" || link.RecommendTier != 1 {
		t.Fatalf("defaults overrode explicit values: %+v", link)
	}
	h.call("PUT", "/admin/api/external-links", map[string]any{"id": id, "anchor_text": "Changed", "target_url": "https://example.com/new", "recommend_tier": 0, "enabled": true}, 200)
	h.db.First(&link, link.ID)
	if link.RecommendTier != 0 {
		t.Fatalf("recommend tier was not updated: %+v", link)
	}
	var snapshotRecord RuntimeExternalRecord
	if err := h.db.Where("link_id = ?", link.ID).First(&snapshotRecord).Error; err != nil || snapshotRecord.RecommendTier != 0 {
		t.Fatalf("snapshot recommend tier was not updated: %+v err=%v", snapshotRecord, err)
	}
	h.call("POST", "/admin/api/external-links", map[string]any{"anchor_text": "Tier two", "target_url": "https://tier.test/", "recommend_tier": 2}, 200)
	h.call("POST", "/admin/api/external-links", map[string]any{"anchor_text": "Bad tier", "target_url": "https://tier.test/", "recommend_tier": -1}, 422)
	before := h.rt.Snapshot().Revision
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "links": "https://good.test/,Good\njavascript:alert(1),Bad"}, 422)
	if h.rt.Snapshot().Revision != before || h.rt.Snapshot().Links[0].AnchorText != "Changed" {
		t.Fatal("invalid import changed live data")
	}
	result := h.call("POST", "/admin/api/external-links/preprocess", map[string]any{"raw_text": "name;url\nWebsite;https://safe.test/\nBad;javascript:alert(1)", "options": map[string]any{"delimiter": ";", "has_header": true, "mapping": map[string]int{"anchor_text": 1, "target_url": 2}, "defaults": map[string]string{"lang": "id"}}}, 200)
	data := result["data"].(map[string]any)
	if data["success_rows"] != float64(1) || data["failed_rows"] != float64(1) {
		t.Fatalf("preprocess contract: %+v", data)
	}
	if h.rt.Snapshot().Revision != before {
		t.Fatal("preview wrote to database")
	}
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "links": data["normalized_text"]}, 200)
	h.call("POST", "/admin/api/internal-domains/import", map[string]any{"domains": "a.test|A|/apps|id|games|50|true|note\na.test|A|/apps|id|games|50|true|note", "strategy": "replace"}, 200)
	if len(h.rt.Snapshot().InternalDomains) != 1 {
		t.Fatal("internal domains were not deduplicated")
	}
	h.call("POST", "/admin/api/main-page-external-links/import", map[string]any{"links": "https://main.test/,Main"}, 200)
	if len(h.rt.Snapshot().MainPageExternalLinks) != 1 {
		t.Fatal("main-page collection missing")
	}
	h.call("POST", "/admin/api/sites/links", map[string]any{"type": "internal", "target_url": "/apps/next", "anchor_text": "Next"}, 200)
	for _, bad := range []string{"//attacker.test", "/%252e%252e/secret", "/a/../b", "https://user:secret@127.0.0.1/", "javascript:alert(1)"} {
		h.call("POST", "/admin/api/sites/links", map[string]any{"type": "internal", "target_url": bad}, 422)
	}
}

func TestNormalizedExternalImportSupportsRecommendTier(t *testing.T) {
	rows, err := splitAdminImportFormat("global|id|Partner|https://partner.test/|demo|100|true|note|1", "normalized")
	if err != nil || len(rows) != 1 || len(rows[0]) != 9 || rows[0][8] != "1" {
		t.Fatalf("normalized tier import: rows=%#v err=%v", rows, err)
	}
	if rows, err := splitAdminImportFormat("global|id|Partner|https://partner.test/|demo|100|true|note|2", "normalized"); err != nil || rows[0][8] != "2" {
		t.Fatalf("tier two import rejected: rows=%#v err=%v", rows, err)
	}
	if _, err := splitAdminImportFormat("global|id|Partner|https://partner.test/|demo|100|true|note|-1", "normalized"); err == nil {
		t.Fatal("negative recommend tier import accepted")
	}
}

func TestAdminPreprocessPreservesRecommendTier(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	result := h.call("POST", "/admin/api/external-links/preprocess", map[string]any{
		"raw_text": "demo.test|id|Tier Two|https://tier2.test/|demo|100|true|note|2",
		"options": map[string]any{
			"delimiter":  "|",
			"has_header": false,
			"mapping":    map[string]int{"domain": 1, "lang": 2, "anchor_text": 3, "target_url": 4, "category": 5, "weight": 6, "enabled": 7, "remark": 8, "recommend_tier": 9},
		},
	}, 200)
	normalized := result["data"].(map[string]any)["normalized_text"].(string)
	if !strings.HasSuffix(normalized, "|2") {
		t.Fatalf("preprocess lost recommend_tier: %q", normalized)
	}
}

func TestAdminHubConfigSubdomainsPersist(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	h.call("POST", "/admin/api/hub/config", map[string]any{"enabled": false, "show_external_panel": true, "subdomain_dsl": "{s3n2}", "subdomain_count": 4}, 200)
	h.call("POST", "/admin/api/hub/config", map[string]any{"subdomain_dsl": "{s99}"}, 422)
	h.call("POST", "/admin/api/hub/subdomains/refresh", map[string]any{"domain": "example.com"}, 200)
	if h.rt.Snapshot().Hub.Config.Enabled || len(h.rt.Snapshot().Hub.Subdomains["example.com"]) != 4 || !h.rt.Snapshot().Config.ShowExternalPanel {
		t.Fatal("hub settings or domain pool not published")
	}
	rt, err := NewRuntime(context.Background(), h.db, RuntimeOptions{LanguageRoot: h.lan, AssetRoot: h.assets})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Snapshot().Hub.Config.Enabled || len(rt.Snapshot().Hub.Subdomains["example.com"]) != 4 {
		t.Fatal("hub state did not survive restart")
	}
}

func TestAdminDatedSnapshotsDeleteRebuildsTodayAndRestart(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "effective_date": "20261101", "source_file": "first.csv", "links": "one.test|id|One|https://one.test/|x|100|true|\ntwo.test|id|Two|https://two.test/|x|50|false|"}, 200)
	s := h.rt.Snapshot()
	if s.Hub.SnapshotMaxIDs["2026-11-01"] != 2 || len(s.Hub.ExternalRecords) != 2 || s.Hub.ExternalRecords[2].Enabled {
		t.Fatalf("incorrect first boundary: %+v", s.Hub)
	}
	firstID := s.Links[0].ID
	if m := s.LinkMetadata[firstID]; m.EffectiveDate != "2026-11-01" || m.SourceFile != "first.csv" || !strings.Contains(m.PHPJSON, `"source_file":"first.csv"`) {
		t.Fatalf("lost PHP import metadata: %+v", m)
	}
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "append", "effective_date": "2026-11-02", "links": "https://three.test/,Three"}, 200)
	if h.rt.Snapshot().Hub.SnapshotMaxIDs["2026-11-01"] != 2 || h.rt.Snapshot().Hub.SnapshotMaxIDs["2026-11-02"] != 3 {
		t.Fatal("append changed the historical boundary")
	}
	today := time.Now().UTC().Format("2006-01-02")
	h.call("DELETE", "/admin/api/external-links", map[string]any{"ids": []uint{firstID}}, 200)
	s = h.rt.Snapshot()
	if len(s.Hub.SnapshotMaxIDs) != 1 || s.Hub.SnapshotMaxIDs[today] != 2 || len(s.Hub.ExternalRecords) != 2 || s.Hub.ActiveDate != today {
		t.Fatalf("PHP delete must rebuild survivors under today, clearing history: %+v", s.Hub)
	}
	if s.Hub.ExternalRecords[1].TargetURL != "https://two.test/" || s.Hub.ExternalRecords[1].Enabled || s.Hub.ExternalRecords[2].TargetURL != "https://three.test/" {
		t.Fatal("delete did not compact snapshot IDs in original insertion order")
	}
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "effective_date": "2026-02-30", "links": "https://bad-date.test/,Bad"}, 422)
	rt, err := NewRuntime(context.Background(), h.db, RuntimeOptions{LanguageRoot: h.lan, AssetRoot: h.assets})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Snapshot().Hub.SnapshotMaxIDs[today] != 2 || len(rt.Snapshot().Hub.SnapshotMaxIDs) != 1 || len(rt.Snapshot().LegacyManagedLinks("external")) != 2 {
		t.Fatal("dated metadata lost on restart")
	}
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "effective_date": "2026-11-03", "links": "https://fresh.test/,Fresh"}, 200)
	s = h.rt.Snapshot()
	if len(s.Hub.SnapshotMaxIDs) != 1 || s.Hub.SnapshotMaxIDs["2026-11-03"] != 1 || s.Hub.ExternalRecords[1].TargetURL != "https://fresh.test/" {
		t.Fatalf("replace did not reset PHP snapshot sequence: %+v", s.Hub)
	}
}

func TestAdminTaskPublicationIsAtomicAcrossProcesses(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	old := h.rt.Snapshot()
	h.call("POST", "/admin/api/lan/persist/jobs", map[string]any{}, 202)
	h.call("POST", "/admin/api/lan/persist/jobs", map[string]any{"force": true}, 409)
	if err := os.WriteFile(filepath.Join(h.lan, "id", "word.txt"), []byte("baru\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorker(h.db, WorkerOptions{LanguageRoot: h.lan})
	if err != nil {
		t.Fatal(err)
	}
	if w.Health(context.Background()) == nil {
		t.Fatal("unstarted worker reported healthy")
	}
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.rt.Snapshot().Bank.Words("id", "abcdef0123456789", 1, 0)[0]; got != "awal" {
		t.Fatal("worker changed in-process bank before revision reload")
	}
	if err := h.rt.ReloadIfChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.rt.Snapshot().Bank.Words("id", "abcdef0123456789", 1, 0)[0]; got != "baru" {
		t.Fatal("completed refresh not published")
	}
	if got := old.Bank.Words("id", "abcdef0123456789", 1, 0)[0]; got != "awal" {
		t.Fatal("old snapshot mutated")
	}
	// A separate web process with no local language directory reads the durable bank.
	restarted, err := NewRuntime(context.Background(), h.db, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.Snapshot().Bank.Words("id", "abcdef0123456789", 1, 0)[0]; got != "baru" {
		t.Fatal("worker publication depends on shared local volume")
	}
	h.call("GET", "/admin/api/lan/persist/status", nil, 200)
}

func TestAdminProductionEnvironmentValidation(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("ADMIN_USERNAME", "operator")
	t.Setenv("ADMIN_PASSWORD", adminTestPassword)
	t.Setenv("ADMIN_PASSWORD_HASH", "")
	t.Setenv("ADMIN_ORIGIN", "")
	if ValidateAdminEnvironment() == nil {
		t.Fatal("production accepted missing origin")
	}
	t.Setenv("ADMIN_ORIGIN", "http://example.com")
	if ValidateAdminEnvironment() == nil {
		t.Fatal("production accepted HTTP")
	}
	t.Setenv("ADMIN_ORIGIN", "https://example.com")
	if err := ValidateAdminEnvironment(); err != nil {
		t.Fatal(err)
	}
	opts, err := AdminOptionsFromEnv()
	if err != nil || !opts.SecureCookies {
		t.Fatal("production HTTPS cookies must be secure")
	}
	for _, password := range []string{"", "password123456", strings.Repeat("x", 20), "changeme12345678"} {
		t.Setenv("ADMIN_PASSWORD", password)
		if ValidateAdminEnvironment() == nil {
			t.Fatal("weak credential accepted")
		}
	}
}

func TestAdminAnonymousSessionBoundAndExpiryReclaimed(t *testing.T) {
	h := newAdminHarness(t, nil)
	for i := 0; i < 20; i++ {
		h.cookie = nil
		w, _ := h.request("GET", "/admin/api/session", nil, "", "")
		if i < 10 && w.Code != 200 {
			t.Fatalf("anonymous challenge %d failed: %d", i, w.Code)
		}
		if i >= 10 && w.Code != 429 {
			t.Fatalf("unbounded anonymous creation: %d", w.Code)
		}
	}
	var count int64
	h.db.Model(&AdminSession{}).Count(&count)
	if count != 10 {
		t.Fatalf("rate limiter created %d sessions", count)
	}
	if err := h.db.Model(&AdminSession{}).Where("user_id = ?", 0).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := CleanupAdminSessions(context.Background(), h.db, 5); err != nil {
		t.Fatal(err)
	}
	h.db.Model(&AdminSession{}).Count(&count)
	if count != 5 {
		t.Fatalf("bounded cleanup removed wrong count: %d", count)
	}
	if err := CleanupAdminSessions(context.Background(), h.db, 200); err != nil {
		t.Fatal(err)
	}
	h.db.Model(&AdminSession{}).Count(&count)
	if count != 0 {
		t.Fatal("expired sessions not reclaimed")
	}
}

func TestAdminGlobalLinkMetadataDoesNotLoosenBankLanguageValidation(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	for _, endpoint := range []string{"/admin/api/external-links", "/admin/api/main-page-external-links"} {
		h.call("POST", endpoint, map[string]any{"lang": "global", "anchor_text": "Global source", "target_url": "https://global.test/"}, 200)
	}
	h.call("POST", "/admin/api/domain-lang-bindings", map[string]any{"domain": "example.com", "lang": "global"}, 422)
	h.call("POST", "/admin/api/lan/persist/jobs", map[string]any{"languages": []string{"global"}}, 422)
	h.call("POST", "/admin/api/sites/config", map[string]any{"default_lang": "global"}, 422)
}

func TestAdminBrandDirectoryBindingAndHubPartialPanelEdit(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	dir := filepath.Join(h.assets, "brand-pool", "awal")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1.jpg"), []byte("image"), 0644); err != nil {
		t.Fatal(err)
	}
	h.call("POST", "/admin/api/domain-image-bindings", map[string]any{"domain": "preview.test", "image_base_url": "/assets/brand-pool"}, 200)
	s := h.rt.Snapshot()
	page, err := BuildLegacyPage(httptest.NewRequest("GET", "http://preview.test/apps/demo", nil), s.Config, LegacySnapshot{LanguageBank: s.Bank, Apps: s.Apps, ImageBindings: s.ImageBindings, LocalImageFiles: s.LocalImageFiles})
	if err != nil {
		t.Fatal(err)
	}
	if page.Images.Primary != "/assets/brand-pool/awal/1.jpg" || len(page.Images.Screenshots) != 1 {
		t.Fatalf("binding did not select reachable brand image: %+v", page.Images)
	}
	h.call("POST", "/admin/api/sites/config", map[string]any{"show_list_panel": true, "show_subdomain_panel": true, "show_external_panel": true}, 200)
	h.call("POST", "/admin/api/hub/config", map[string]any{"subdomain_count": 21}, 200)
	rt, err := NewRuntime(context.Background(), h.db, RuntimeOptions{LanguageRoot: h.lan})
	if err != nil {
		t.Fatal(err)
	}
	cfg := rt.Snapshot().Config
	if !cfg.ShowListPanel || !cfg.ShowSubdomainPanel || !cfg.ShowExternalPanel {
		t.Fatalf("unrelated hub edit overwrote panels: %+v", cfg)
	}
}

func TestAdminRefreshEventsRetainFailureAfterSuccess(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	h.call("POST", "/admin/api/lan/persist/jobs", map[string]any{}, 202)
	if err := os.Rename(filepath.Join(h.lan, "id", "word.txt"), filepath.Join(h.lan, "id", "saved.txt")); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorker(h.db, WorkerOptions{LanguageRoot: h.lan})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(context.Background()); err == nil {
		t.Fatal("missing words unexpectedly refreshed")
	}
	var job RefreshJob
	h.db.First(&job)
	if err := h.db.Model(&job).Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(h.lan, "id", "saved.txt"), filepath.Join(h.lan, "id", "word.txt")); err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.db.First(&job, "id = ?", job.ID)
	if job.Status != "done" || job.LastError != "" {
		t.Fatalf("expected recovered success: %+v", job)
	}
	var events []RefreshJobEvent
	h.db.Where("job_id = ?", job.ID).Order("id").Find(&events)
	failed, succeeded := false, false
	for _, e := range events {
		if e.Status == "retry" && strings.Contains(e.Message, "words") {
			failed = true
		}
		if e.Status == "done" {
			succeeded = true
		}
	}
	if !failed || !succeeded {
		t.Fatalf("retry diagnostics lost: %+v", events)
	}
	for i := 0; i < 40; i++ {
		if err := recordRefreshEvent(h.db, job, "test", "bounded"); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	h.db.Model(&RefreshJobEvent{}).Where("job_id = ?", job.ID).Count(&count)
	if count != 32 {
		t.Fatalf("unbounded event history: %d", count)
	}
}

// TEST_MYSQL_DSN must name a disposable integration database. Every table uses
// an isolated per-test prefix and is dropped before the connection is closed.
func TestAdminOptionalMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not configured")
	}
	prefix := fmt.Sprintf("admin_it_%d_", time.Now().UnixNano())
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}})
	if err != nil {
		t.Fatal("connect optional MySQL:", err)
	}
	h := newAdminHarness(t, db)
	t.Cleanup(func() {
		for _, model := range []any{&RuntimeLinkMetadata{}, &RuntimeExternalRecord{}, &RuntimeExternalDate{}, &RuntimeHubSubdomain{}, &RuntimeHubState{}, &WorkerHeartbeat{}, &RefreshJobEvent{}, &RefreshJob{}, &LanguageVersion{}, &RuntimeRevision{}, &InternalDomain{}, &AdminSession{}, &AdminUser{}, &AppScreenshot{}, &App{}, &SiteConfig{}, &SiteLink{}, &DomainBinding{}, &DomainImageBinding{}} {
			if err := db.Migrator().DropTable(model); err != nil {
				t.Error(err)
			}
		}
	})
	verifyRuntimePHPIDMigration(t, h)
	h.login()
	h.call("POST", "/admin/api/internal-domains/import", map[string]any{"domains": "mysql.test|MySQL|/|id|test|0|false|integration"}, 200)
	h.call("POST", "/admin/api/lan/persist/jobs", map[string]any{}, 202)
	w, err := NewWorker(db, WorkerOptions{LanguageRoot: h.lan})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.rt.ReloadIfChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	var job RefreshJob
	db.First(&job)
	if job.Status != "done" {
		t.Fatalf("MySQL worker status %s: %s", job.Status, job.LastError)
	}
	expired := time.Now().UTC().Add(-time.Minute)
	crashed := RefreshJob{ID: "mysql-final-crash", Languages: `["id"]`, Status: "running", Attempts: 1, MaxAttempts: 1, AvailableAt: expired, LeaseOwner: "dead", LeaseUntil: &expired}
	if err := db.Create(&crashed).Error; err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	crashed = RefreshJob{ID: crashed.ID}
	if err := db.First(&crashed, "id = ?", crashed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if crashed.Status != "failed" || crashed.LeaseUntil != nil {
		t.Fatalf("MySQL final crash not reclaimed: %+v", crashed)
	}
	queued, err := EnqueueRefresh(context.Background(), db, []string{"id"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if queued.ID == "" {
		t.Fatal("new admission blocked after expired final lease")
	}
	h.call("POST", "/admin/api/sites/config", map[string]any{"show_external_panel": true}, 200)
	h.call("POST", "/admin/api/hub/config", map[string]any{"subdomain_count": 21}, 200)
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "effective_date": "2026-11-25", "links": "https://mysql.test/,MySQL"}, 200)
	reloaded, err := NewRuntime(context.Background(), db, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Snapshot().Config.ShowExternalPanel || reloaded.Snapshot().Hub.SnapshotMaxIDs["2026-11-25"] != 1 {
		t.Fatal("MySQL config or dated boundary lost after restart")
	}
}
