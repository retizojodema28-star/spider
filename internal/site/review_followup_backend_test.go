package site

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestReviewFollowupPreprocessImportPreservesTarget(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	r := h.call("POST", "/admin/api/external-links/preprocess", map[string]any{
		"raw_text": "See <a href='https://wrong.test/'>manual</a>;https://intended.test/",
		"options":  map[string]any{"delimiter": ";", "mapping": map[string]int{"anchor_text": 1, "target_url": 2}, "defaults": map[string]any{"lang": "id", "weight": 7, "enabled": false}},
	}, 200)
	d := r["data"].(map[string]any)
	if d["success_rows"] != float64(1) || d["failed_rows"] != float64(0) {
		t.Fatalf("preprocess failed: %v", d)
	}
	if d["import_format"] != "normalized" {
		t.Fatalf("preprocess must identify its explicit import format: %v", d)
	}
	t.Logf("preview=%v normalized=%s", d["preview"], d["normalized_text"])
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "links": d["normalized_text"], "format": d["import_format"]}, 200)
	var row SiteLink
	if err := h.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("stored target=%s language=%s weight=%d enabled=%v", row.TargetURL, row.Language, row.Weight, row.Enabled)
	if row.TargetURL != "https://intended.test/" || row.Language != "id" || row.Weight != 7 || row.Enabled {
		t.Errorf("normalized preview changed target/metadata when imported: %+v", row)
	}
}

func TestReviewFollowupLoginAttemptsUseSameSession(t *testing.T) {
	h := newAdminHarness(t, nil)
	s := h.call("GET", "/admin/api/session", nil, 200)
	h.csrf = s["data"].(map[string]any)["csrf_token"].(string)
	started := time.Now()
	statuses := map[int]int{}
	var lookups atomic.Int32
	if err := h.db.Callback().Query().Before("gorm:query").Register("followup_login_queries", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "admin_users" {
			lookups.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		endpoint := "/admin/api/session/login"
		if i%2 != 0 {
			endpoint = "/admin/api/session/verify"
		}
		w, _ := h.request("POST", endpoint, map[string]any{"username": "nonexistent-review-user", "password": "incorrect"}, "http://preview.test", h.csrf)
		statuses[w.Code]++
		if w.Code == 429 && w.Header().Get("Retry-After") == "" {
			t.Error("throttle response missing retry hint")
		}
	}
	var count int64
	if err := h.db.Model(&AdminSession{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("25 attempts with one anonymous session: statuses=%v sessions=%d elapsed=%v", statuses, count, time.Since(started))
	if statuses[429] != 15 || statuses[401] != 10 || lookups.Load() != 10 {
		t.Errorf("shared alias limit must reject before user lookup/bcrypt: statuses=%v lookups=%d", statuses, lookups.Load())
	}
}

func TestReviewFollowupGenericLinksRejectInternalDomain(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	before := h.rt.Snapshot().Revision
	for _, method := range []string{"POST", "PUT"} {
		for _, kind := range []string{"internal-domain", "main-external", "invalid"} {
			w, _ := h.request(method, "/admin/api/sites/links", map[string]any{"id": 123, "type": kind, "domain": "intended.test", "title": "Expected domain", "path": "/"}, "http://preview.test", h.csrf)
			if w.Code != 422 {
				t.Errorf("%s type=%s: status=%d body=%s", method, kind, w.Code, w.Body.String())
			}
		}
	}
	var links []SiteLink
	var domains []InternalDomain
	if err := h.db.Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Find(&domains).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 || len(domains) != 0 || workerReviewRevision(t, h.db) != before {
		t.Error("rejected collection changed database or revision")
	}
}

func TestReviewFollowupWorkerRejectsFIFOWithoutBlocking(t *testing.T) {
	db, root := workerReviewDB(t), t.TempDir()
	workerReviewBank(t, root, "id", "old")
	fifo := filepath.Join(root, "id", "sentences.txt")
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	w, err := NewWorker(db, WorkerOptions{LanguageRoot: root, LeaseDuration: 600 * time.Millisecond, PollInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	job := workerReviewEnqueue(t, db, 1, "id")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.RunOnce(ctx) }()
	// A regular-file rejection should complete before this; also allow multiple
	// lease renewals to prove this is permanently occupied rather than expiring.
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular") {
			t.Fatalf("expected a special-file error, got %v", err)
		}
		failed := workerReviewJob(t, db, job.ID)
		if failed.Status != "failed" || failed.LeaseOwner != "" || failed.LeaseUntil != nil {
			t.Fatalf("special-file error did not release job: %+v", failed)
		}
		if _, err := EnqueueRefresh(context.Background(), db, []string{"id"}, 1); err != nil {
			t.Fatalf("special-file error blocked subsequent refresh: %v", err)
		}
		return
	case <-time.After(1300 * time.Millisecond):
	}
	before := workerReviewJob(t, db, job.ID)
	health := w.Health(context.Background())
	_, busy := EnqueueRefresh(context.Background(), db, []string{"id"}, 1)
	cancel()
	select {
	case err := <-done:
		t.Logf("cancellation returned: %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Log("cancellation did not unblock the file open")
	}
	// Open the FIFO from the test solely to release the reader and avoid leaking
	// the blocked goroutine. No live file or database is used.
	fd, openErr := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0600)
	if openErr == nil {
		syscall.Close(fd)
	}
	select {
	case err := <-done:
		t.Logf("after releasing FIFO: %v", err)
	case <-time.After(time.Second):
		t.Fatalf("cleanup failed: FIFO open=%v", openErr)
	}
	t.Errorf("FIFO stalls job and keeps renewing: status=%s attempts=%d liveLease=%v health=%v newJob=%v", before.Status, before.Attempts, before.LeaseUntil != nil && before.LeaseUntil.After(time.Now().Add(-300*time.Millisecond)), health, busy)
}

func TestReviewFollowupImportedAnchorRowLimit(t *testing.T) {
	for _, format := range []string{"html", "delimited", "mixed"} {
		t.Run(format, func(t *testing.T) {
			var b strings.Builder
			for i := 0; i < 10000; i++ {
				if format == "html" || (format == "mixed" && i%2 == 0) {
					fmt.Fprintf(&b, "<a href='https://example.test/%d'>A</a>\n", i)
				} else {
					fmt.Fprintf(&b, "example.test|id|A|https://example.test/%d||7|false|\n", i)
				}
			}
			rows, err := splitAdminImport(b.String())
			if err != nil || len(rows) != 10000 {
				t.Fatalf("10000 rows: count=%d error=%v", len(rows), err)
			}
			b.WriteString("<a href='https://example.test/10000'>A</a>\n")
			if _, err := splitAdminImport(b.String()); err == nil {
				t.Fatal("10001 rows accepted")
			}
		})
	}
}

func TestReviewFollowupCanonicalProductionOrigin(t *testing.T) {
	for _, tc := range []struct{ configured, canonical string }{
		{"https://PREVIEW.test", "https://preview.test"},
		{"https://preview.test:443", "https://preview.test"},
		{"https://preview.test:0443", "https://preview.test"},
		{"https://PREVIEW.test:8443", "https://preview.test:8443"},
		{"https://[::1]:443", "https://[::1]"},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			h := newAdminHarness(t, nil)
			t.Setenv("APP_ENV", "production")
			t.Setenv("ADMIN_ORIGIN", tc.configured)
			opts, err := AdminOptionsFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if opts.Origin != tc.canonical || !opts.SecureCookies {
				t.Fatalf("options: %+v", opts)
			}
			h.router = gin.New()
			if err := RegisterAdminRoutes(h.router, h.rt, opts); err != nil {
				t.Fatal(err)
			}
			s := h.call("GET", "/admin/api/session", nil, 200)
			h.csrf = s["data"].(map[string]any)["csrf_token"].(string)
			for _, wrong := range []string{"http://preview.test", "https://other.test", "https://preview.test:9443", "https://preview.test.evil", "https://preview.test.", "null"} {
				w, _ := h.request("POST", "/admin/api/session/login", map[string]any{"username": "operator", "password": adminTestPassword}, wrong, h.csrf)
				if w.Code != 403 {
					t.Errorf("different origin %s accepted: %d", wrong, w.Code)
				}
			}
			if tc.canonical == "https://preview.test:8443" {
				w, _ := h.request("POST", "/admin/api/session/login", map[string]any{"username": "operator", "password": adminTestPassword}, "https://preview.test", h.csrf)
				if w.Code != 403 {
					t.Error("nondefault port collapsed")
				}
			}
			w, _ := h.request("POST", "/admin/api/session/login", map[string]any{"username": "operator", "password": adminTestPassword}, tc.canonical, h.csrf)
			if w.Code != 200 {
				t.Error("valid production origin makes browser-serialized origin fail CSRF checks")
			}
		})
	}
}

func TestReviewFollowupImportStorageFailureIsAtomic(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "links": "https://kept.test/,Kept", "effective_date": "2026-11-01"}, 200)
	before := h.rt.Snapshot()
	name := "review_followup_inject_link_insert_failure"
	insertions := 0
	if err := h.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "site_links" {
			insertions++
			if insertions == 2 {
				tx.AddError(fmt.Errorf("review injected second insert failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.call("POST", "/admin/api/sites/links/import", map[string]any{"type": "external", "strategy": "replace", "links": "https://new-one.test/,One\nhttps://new-two.test/,Two", "effective_date": "2026-11-02"}, 503)
	if err := h.db.Callback().Create().Remove(name); err != nil {
		t.Fatal(err)
	}
	if insertions != 2 {
		t.Fatalf("fault did not fire after first inserted row: %d", insertions)
	}
	if h.rt.Snapshot() != before {
		t.Fatal("failed replace changed published snapshot")
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := h.rt.Snapshot()
	if after.Revision != before.Revision || len(after.Links) != 1 || after.Links[0].TargetURL != "https://kept.test/" || after.Hub.ActiveDate != "2026-11-01" || len(after.Hub.ExternalRecords) != 1 {
		t.Fatalf("partial replace survived rollback: %+v", after)
	}
	t.Log("second-row SQL failure rolled back deletion, first insert, metadata, historical snapshots and revision")
}

func TestReviewFollowupCommittedWriteReloadFailureRecovers(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	before := h.rt.Snapshot()
	name := "review_followup_inject_snapshot_failure"
	if err := h.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "apps" {
			tx.AddError(fmt.Errorf("review injected snapshot read failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	w, _ := h.request("POST", "/admin/api/sites/config", map[string]any{"site_name": "Committed new name"}, "http://preview.test", h.csrf)
	if err := h.db.Callback().Query().Remove(name); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("X-Runtime-Reload") != "pending" || h.rt.Snapshot() != before {
		t.Fatalf("failed reload response=%d header=%s snapshotSame=%v", w.Code, w.Header().Get("X-Runtime-Reload"), h.rt.Snapshot() == before)
	}
	if err := h.rt.ReloadIfChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.rt.Snapshot().Config.DisplayName != "Committed new name" || h.rt.Snapshot().Revision != before.Revision+1 {
		t.Fatal("poll did not recover committed config")
	}
	t.Log("committed write reports reload pending, preserves old snapshot, and revision poll publishes new value")
}

func TestReviewFollowupRawAnchorsAndStructuredFields(t *testing.T) {
	for _, tc := range []struct{ raw, target, label string }{
		{`<a href="https://raw.test/?a=1&amp;b=2">Raw <b>label</b>, pipe|tab</a>`, "https://raw.test/?a=1&b=2", "Raw label, pipe|tab"},
		{`See <span><a href="https://raw.test/">Wrapped</a></span> tail`, "https://raw.test/", "Wrapped"},
	} {
		rows, err := splitAdminImport(tc.raw)
		if err != nil || !reflect.DeepEqual(rows, [][]string{{tc.target, tc.label}}) {
			t.Fatalf("raw anchor changed: %q rows=%v err=%v", tc.raw, rows, err)
		}
	}
	// These are the exact output formats emitted by csv.Writer in preprocess,
	// including markup in either label/remark and delimiters within quoted data.
	for _, delimiter := range []rune{'|', '\t', ','} {
		fields := []string{"intended.test", "id", `See <a href="https://wrong.test/">manual</a>, | tab` + "\ttext", "https://intended.test/", "cat", "7", "false", `<a href='https://remark.test/'>remark</a>`}
		var out strings.Builder
		writer := csv.NewWriter(&out)
		writer.Comma = delimiter
		if err := writer.Write(fields); err != nil {
			t.Fatal(err)
		}
		writer.Flush()
		rows, err := splitAdminImportFormat(out.String(), "normalized")
		if err != nil || !reflect.DeepEqual(rows, [][]string{fields}) {
			t.Fatalf("structured row delimiter=%q: %v err=%v", delimiter, rows, err)
		}
	}
	malformed := `intended.test|id|"unterminated <a href='https://wrong.test/'>A</a>|https://intended.test/||7|false|`
	if _, err := splitAdminImportFormat(malformed, "normalized"); err == nil {
		t.Fatal("malformed structured quoting fell back to an embedded anchor")
	}
}

func TestReviewFollowupLoginConcurrencyAcrossAliases(t *testing.T) {
	h := newAdminHarness(t, nil)
	s := h.call("GET", "/admin/api/session", nil, 200)
	csrf := s["data"].(map[string]any)["csrf_token"].(string)
	cookie := *h.cookie
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var lookups atomic.Int32
	if err := h.db.Callback().Query().Before("gorm:query").Register("followup_login_concurrency", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "admin_users" {
			lookups.Add(1)
			entered <- struct{}{}
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"username": "unknown-concurrency-user", "password": "incorrect"})
	request := func(i int) int {
		endpoint := "/admin/api/session/login"
		if i%2 != 0 {
			endpoint = "/admin/api/session/verify"
		}
		r := httptest.NewRequest("POST", "http://preview.test"+endpoint, bytes.NewReader(body))
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
		r.Header.Set("Origin", "http://preview.test")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		r.AddCookie(&cookie)
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, r)
		return w.Code
	}
	active := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func(i int) { active <- request(i) }(i)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("initial login admission stalled")
		}
	}
	rejected := make(chan int, 16)
	for i := 4; i < 20; i++ {
		go func(i int) { rejected <- request(i) }(i)
	}
	for i := 0; i < 16; i++ {
		select {
		case status := <-rejected:
			if status != 429 {
				t.Errorf("concurrent overflow status=%d", status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("overflow reached blocked authentication work")
		}
	}
	if lookups.Load() != 4 {
		t.Errorf("overflow performed user lookup: %d", lookups.Load())
	}
	once.Do(func() { close(release) })
	for i := 0; i < 4; i++ {
		select {
		case status := <-active:
			if status != 401 {
				t.Errorf("admitted status=%d", status)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("admitted request did not finish")
		}
	}
	if got := request(21); got != 401 {
		t.Errorf("completed requests did not release concurrency admission: %d", got)
	}
}

func TestReviewFollowupRegularFileOpenRejectsFIFOAndSymlink(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regular, []byte("regular"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openRuntimeRegularFile(regular)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.txt")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, link, dir} {
		done := make(chan error, 1)
		go func() {
			f, err := openRuntimeRegularFile(path)
			if f != nil {
				f.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("unsafe input opened: %s", path)
			}
		case <-time.After(time.Second):
			// Release a regressed blocking FIFO implementation before failing.
			fd, e := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0600)
			if e == nil {
				syscall.Close(fd)
			}
			t.Fatalf("special-file open blocked: %s", path)
		}
	}
}
