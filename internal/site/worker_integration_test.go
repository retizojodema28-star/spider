package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// These tests exercise the persisted state machine independently of cmd/worker.
// HTTP authentication and actual MySQL/process isolation are additionally tested
// by scripts/worker_acceptance.py. No production functions are replaced.
func workerReviewDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testSiteDB(t)
	if err := MigrateAdmin(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func workerReviewBank(t *testing.T, root, lang, word string) {
	t.Helper()
	dir := filepath.Join(root, lang)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for file, content := range map[string]string{
		"word.txt":      word + "\n" + word + "\n",
		"sentences.txt": "A complete acceptance sentence for " + word + ".\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func workerReviewWorker(t *testing.T, db *gorm.DB, root string, languages ...string) *Worker {
	t.Helper()
	w, err := NewWorker(db, WorkerOptions{LanguageRoot: root, Languages: languages,
		LeaseDuration: 10 * time.Second, RetryDelay: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func workerReviewJob(t *testing.T, db *gorm.DB, id string) RefreshJob {
	t.Helper()
	var job RefreshJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func workerReviewRevision(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	var revision RuntimeRevision
	if err := db.First(&revision, 1).Error; err != nil {
		t.Fatal(err)
	}
	return revision.Revision
}

func workerReviewEnqueue(t *testing.T, db *gorm.DB, attempts int, languages ...string) RefreshJob {
	t.Helper()
	job, err := EnqueueRefresh(context.Background(), db, languages, attempts)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func workerReviewVersion(t *testing.T, db *gorm.DB, lang string) LanguageVersion {
	t.Helper()
	var version LanguageVersion
	if err := db.First(&version, "language = ?", lang).Error; err != nil {
		t.Fatal(err)
	}
	return version
}

func TestWorkerReviewPublishesAndRestartsWithoutLocalFiles(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	workerReviewBank(t, root, "id", "original")
	rt, err := NewRuntime(ctx, db, RuntimeOptions{LanguageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	old := rt.Snapshot()
	workerReviewBank(t, root, "id", "replacement")
	job := workerReviewEnqueue(t, db, 3, "id")
	w := workerReviewWorker(t, db, root)
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got := workerReviewJob(t, db, job.ID)
	if got.Status != "done" || got.Attempts != 1 || got.Phase != "finished" || got.FinishedAt == nil || got.StartedAt == nil || got.LeaseOwner != "" || got.LeaseUntil != nil || got.LastError != "" {
		t.Fatalf("unexpected completed job: %+v", got)
	}
	var stats map[string]struct{ Words, Sentences int }
	if err := json.Unmarshal([]byte(got.Result), &stats); err != nil || stats["id"].Words != 1 || stats["id"].Sentences != 1 {
		t.Fatalf("result=%s err=%v", got.Result, err)
	}
	v := workerReviewVersion(t, db, "id")
	if v.JobID != job.ID || !strings.Contains(v.Payload, "replacement") || workerReviewRevision(t, db) != old.Revision+1 {
		t.Fatalf("publication/revision not committed together: %+v", v)
	}
	if rt.Snapshot() != old || old.Bank.languages["id"].words[0] != "original" {
		t.Fatal("worker modified an already published in-memory snapshot")
	}
	if err := rt.ReloadIfChanged(ctx); err != nil {
		t.Fatal(err)
	}
	if rt.Snapshot().Bank.languages["id"].words[0] != "replacement" {
		t.Fatal("web runtime failed to observe worker publication")
	}
	restarted, err := NewRuntime(ctx, db, RuntimeOptions{LanguageRoot: filepath.Join(t.TempDir(), "missing")})
	if err != nil || restarted.Snapshot().Revision != old.Revision+1 || restarted.Snapshot().Bank.languages["id"].words[0] != "replacement" {
		t.Fatalf("restart did not use persisted bank: %v", err)
	}
	if err := w.RunOnce(ctx); err != nil || workerReviewRevision(t, db) != old.Revision+1 {
		t.Fatalf("idle tick changed publication: %v", err)
	}
}

func TestWorkerReviewPartialFailurePreservesSnapshotAndAdminRetry(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	workerReviewBank(t, root, "id", "oldbank")
	w := workerReviewWorker(t, db, root, "id", "en")
	workerReviewEnqueue(t, db, 1, "id")
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(ctx, db, RuntimeOptions{LanguageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	old, version := rt.Snapshot(), workerReviewVersion(t, db, "id")
	workerReviewBank(t, root, "id", "newbank")
	job := workerReviewEnqueue(t, db, 1, "id", "en")
	if err := w.RunOnce(ctx); err == nil {
		t.Fatal("missing second language unexpectedly succeeded")
	}
	failed := workerReviewJob(t, db, job.ID)
	if failed.Status != "failed" || failed.Attempts != 1 || failed.FinishedAt == nil || failed.LastError == "" || failed.LeaseOwner != "" || failed.LeaseUntil != nil {
		t.Fatalf("failure not persisted: %+v", failed)
	}
	if workerReviewRevision(t, db) != old.Revision || !reflect.DeepEqual(workerReviewVersion(t, db, "id"), version) {
		t.Fatal("failed multi-language refresh partially published")
	}
	if err := rt.ReloadIfChanged(ctx); err != nil || rt.Snapshot() != old {
		t.Fatalf("failed refresh replaced last good snapshot: %v", err)
	}
	// Exercise the real retry handler; auth is covered by the live acceptance.
	router := gin.New()
	a := &adminServer{runtime: rt}
	router.POST("/jobs/:id/retry", a.retry)
	retry := func(id string, status int) {
		t.Helper()
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest("POST", "/jobs/"+id+"/retry", nil))
		if r.Code != status {
			t.Fatalf("retry status=%d, want %d: %s", r.Code, status, r.Body.String())
		}
	}
	retry("missing-job", 404)
	retry(job.ID, 200)
	queued := workerReviewJob(t, db, job.ID)
	if queued.Status != "queued" || queued.Attempts != 0 || queued.FinishedAt != nil {
		t.Fatalf("retry did not reset terminal state: %+v", queued)
	}
	retry(job.ID, 409)
	workerReviewBank(t, root, "en", "english")
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := workerReviewJob(t, db, job.ID); got.Status != "done" || got.Attempts != 1 || got.LastError != "" {
		t.Fatalf("retry did not recover: %+v", got)
	}
	if workerReviewRevision(t, db) != old.Revision+1 || workerReviewVersion(t, db, "en").JobID != job.ID {
		t.Fatal("retry must publish both languages at one revision")
	}
	retry(job.ID, 404)
}

func TestWorkerReviewAutomaticRetryHonorsBackoff(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	w := workerReviewWorker(t, db, root)
	job := workerReviewEnqueue(t, db, 3)
	for attempt := 1; attempt <= 2; attempt++ {
		before := time.Now().UTC()
		if err := w.RunOnce(ctx); err == nil {
			t.Fatal("missing language input unexpectedly succeeded")
		}
		got := workerReviewJob(t, db, job.ID)
		if got.Status != "retry" || got.Attempts != attempt || got.FinishedAt != nil || got.AvailableAt.Before(before.Add(time.Hour*time.Duration(1<<(attempt-1)))) {
			t.Fatalf("retry/backoff incorrect: %+v", got)
		}
		if err := w.RunOnce(ctx); err != nil || workerReviewJob(t, db, job.ID).Attempts != attempt {
			t.Fatalf("future retry was claimed: %v", err)
		}
		if err := db.Model(&RefreshJob{}).Where("id = ?", job.ID).Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	workerReviewBank(t, root, "id", "recovered")
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := workerReviewJob(t, db, job.ID); got.Status != "done" || got.Attempts != 3 || got.LastError != "" {
		t.Fatalf("automatic retry did not recover: %+v", got)
	}
	if workerReviewRevision(t, db) != 2 {
		t.Fatal("failed attempts incremented publication revision")
	}
}

func TestWorkerReviewFailureDiagnosticsSurviveSuccessfulRetry(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	job := workerReviewEnqueue(t, db, 2)
	w := workerReviewWorker(t, db, root)
	cause := w.RunOnce(ctx)
	if cause == nil {
		t.Fatal("missing input must fail the first attempt")
	}
	failed := workerReviewJob(t, db, job.ID)
	if failed.Status != "retry" || failed.LastError == "" {
		t.Fatalf("first attempt missing retry diagnostic: %+v", failed)
	}
	if err := db.Model(&RefreshJob{}).Where("id = ?", job.ID).Update("available_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	workerReviewBank(t, root, "id", "diagnostic-recovery")
	// A fresh worker must recover using only persisted state.
	if err := workerReviewWorker(t, db, root).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	completed := workerReviewJob(t, db, job.ID)
	if completed.Status != "done" || completed.Attempts != 2 {
		t.Fatalf("retry did not complete: %+v", completed)
	}
	var events []RefreshJobEvent
	if err := db.Where("job_id = ?", job.ID).Order("id asc").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	failureRetained, successRetained := false, false
	for _, event := range events {
		if event.Attempt == 1 && event.Status == "retry" && event.Message == failed.LastError && !event.CreatedAt.IsZero() {
			failureRetained = true
		}
		if event.Attempt == 2 && event.Status == "done" && !event.CreatedAt.IsZero() {
			successRetained = true
		}
	}
	if !failureRetained || !successRetained {
		t.Fatalf("failure-to-success history lost: failure=%v success=%v events=%+v", failureRetained, successRetained, events)
	}
	// Schema/bootstrap operations during restart must preserve prior diagnostics.
	if err := MigrateAdmin(db); err != nil {
		t.Fatal(err)
	}
	var after []RefreshJobEvent
	if err := db.Where("job_id = ?", job.ID).Order("id asc").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, after) {
		t.Fatal("restart migration changed persisted attempt diagnostics")
	}
}

func TestWorkerReviewExpiredFinalLeaseBecomesTerminalWithoutOtherJobs(t *testing.T) {
	ctx := context.Background()
	db := workerReviewDB(t)
	job := workerReviewEnqueue(t, db, 1)
	old := workerReviewWorker(t, db, t.TempDir())
	if claimed, err := old.claim(ctx); err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := db.Model(&RefreshJob{}).Where("id = ?", job.ID).Update("lease_until", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	w := workerReviewWorker(t, db, t.TempDir())
	for i := 0; i < 2; i++ {
		if err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got := workerReviewJob(t, db, job.ID)
	if got.Status != "failed" || got.FinishedAt == nil || got.LeaseOwner != "" || got.LeaseUntil != nil || !strings.Contains(got.LastError, "expired") {
		t.Errorf("exhausted expired lease must commit terminal failure even with no next job; got status=%s attempts=%d finished=%v owner=%q error=%q", got.Status, got.Attempts, got.FinishedAt, got.LeaseOwner, got.LastError)
	}
	if _, err := EnqueueRefresh(ctx, db, []string{"id"}, 1); err != nil {
		t.Errorf("exhausted expired lease blocks all future refreshes: %v", err)
	}
}

func TestWorkerReviewExpiredLeaseFencesPreviousOwner(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	workerReviewBank(t, root, "id", "takeover")
	job := workerReviewEnqueue(t, db, 3)
	w1, w2 := workerReviewWorker(t, db, root), workerReviewWorker(t, db, root)
	first, err := w1.claim(ctx)
	if err != nil || first == nil {
		t.Fatalf("first claim: %v", err)
	}
	if next, err := w2.claim(ctx); err != nil || next != nil {
		t.Fatalf("live lease stolen: %+v %v", next, err)
	}
	if err := db.Model(&RefreshJob{}).Where("id = ?", job.ID).Update("lease_until", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := w1.renew(ctx, first); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired lease renewed: %v", err)
	}
	second, err := w2.claim(ctx)
	if err != nil || second == nil || second.Attempts != 2 || second.LeaseOwner != w2.id {
		t.Fatalf("takeover failed: %+v %v", second, err)
	}
	banks := map[string]languageData{"id": {words: []string{"stale"}, sentences: []string{"Stale worker must never publish this sentence."}}}
	for name, action := range map[string]func() error{
		"renew":  func() error { return w1.renew(ctx, first) },
		"fail":   func() error { return w1.fail(ctx, first, errors.New("old owner")) },
		"finish": func() error { return w1.finish(ctx, first, banks) },
	} {
		if err := action(); !errors.Is(err, ErrLeaseLost) {
			t.Errorf("stale owner %s = %v, want ErrLeaseLost", name, err)
		}
	}
	var count int64
	if err := db.Model(&LanguageVersion{}).Count(&count).Error; err != nil || count != 0 || workerReviewRevision(t, db) != 1 {
		t.Fatalf("stale finish partially committed: count=%d err=%v", count, err)
	}
	banks["id"] = languageData{words: []string{"current"}, sentences: []string{"Current owner publishes its complete sentence."}}
	if err := w2.finish(ctx, second, banks); err != nil {
		t.Fatal(err)
	}
	if got := workerReviewJob(t, db, job.ID); got.Status != "done" || got.Attempts != 2 {
		t.Fatalf("reclaimed job failed: %+v", got)
	}
	if workerReviewRevision(t, db) != 2 || !strings.Contains(workerReviewVersion(t, db, "id").Payload, "current") {
		t.Fatal("new owner publication missing")
	}
}

func TestWorkerReviewLeaseExpiringDuringPublicationRollsBack(t *testing.T) {
	ctx := context.Background()
	db := workerReviewDB(t)
	job := workerReviewEnqueue(t, db, 2)
	w, err := NewWorker(db, WorkerOptions{LeaseDuration: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := w.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	// Delay the actual version write past the lease deadline. finish must fence
	// using the time of its terminal update, not the start of the transaction.
	callback := "worker_review_slow_publication"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "language_versions" {
			time.Sleep(max(0, time.Until(*claimed.LeaseUntil)) + 25*time.Millisecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	banks := map[string]languageData{"id": {words: []string{"late"}, sentences: []string{"This publication must fail after its lease expires."}}}
	err = w.finish(ctx, claimed, banks)
	if removeErr := db.Callback().Create().Remove(callback); removeErr != nil {
		t.Fatal(removeErr)
	}
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("publication after lease expiry = %v, want ErrLeaseLost", err)
	}
	var versions, successes int64
	if err := db.Model(&LanguageVersion{}).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RefreshJobEvent{}).Where("job_id = ? AND status = ?", job.ID, "done").Count(&successes).Error; err != nil {
		t.Fatal(err)
	}
	if versions != 0 || successes != 0 || workerReviewRevision(t, db) != 1 || workerReviewJob(t, db, job.ID).Status != "running" {
		t.Fatal("expired publication committed a bank, revision, completion or success diagnostic")
	}
}

func TestWorkerReviewConcurrentClaimAndCompletionPublishExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	// SQLite omits SELECT FOR UPDATE. Begin its single-writer transactions
	// immediately so contenders wait at the database lock, instead of deadlocking
	// while upgrading simultaneous read transactions. The assertions stay strict;
	// the live MySQL harness separately races two real worker processes.
	dsn := db.Dialector.(*sqlite.Dialector).DSN
	var err error
	db, err = gorm.Open(sqlite.Open(dsn+"?_txlock=immediate&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	workerReviewBank(t, root, "id", "concurrent")
	job := workerReviewEnqueue(t, db, 3)
	const n = 8
	workers := make([]*Worker, n)
	for i := range workers {
		workers[i] = workerReviewWorker(t, db, root)
	}
	type outcome struct {
		worker *Worker
		job    *RefreshJob
		err    error
	}
	results := make(chan outcome, n)
	start := make(chan struct{})
	for _, w := range workers {
		go func(w *Worker) { <-start; j, err := w.claim(ctx); results <- outcome{w, j, err} }(w)
	}
	close(start)
	var owner outcome
	claims := 0
	for range workers {
		got := <-results
		if got.err != nil {
			t.Errorf("concurrent claim: %v", got.err)
		}
		if got.job != nil && got.err == nil {
			owner = got
			claims++
		}
	}
	if claims != 1 || workerReviewJob(t, db, job.ID).Attempts != 1 {
		t.Fatalf("claims=%d, want exactly one", claims)
	}
	banks := map[string]languageData{"id": {words: []string{"concurrent"}, sentences: []string{"Concurrent completions publish only one revision."}}}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- owner.worker.finish(ctx, owner.job, banks) }()
	}
	success, fenced := 0, 0
	for i := 0; i < 2; i++ {
		err := <-done
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrLeaseLost):
			fenced++
		default:
			t.Errorf("finish: %v", err)
		}
	}
	if success != 1 || fenced != 1 || workerReviewRevision(t, db) != 2 {
		t.Fatalf("completion success=%d fenced=%d revision=%d", success, fenced, workerReviewRevision(t, db))
	}
}

func TestWorkerReviewPublicationSQLFailureRollsBackBankAndRevision(t *testing.T) {
	ctx := context.Background()
	db, root := workerReviewDB(t), t.TempDir()
	workerReviewBank(t, root, "id", "before")
	w := workerReviewWorker(t, db, root)
	workerReviewEnqueue(t, db, 1)
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	before := workerReviewVersion(t, db, "id")
	workerReviewBank(t, root, "id", "after")
	job := workerReviewEnqueue(t, db, 1)
	if err := db.Exec(`CREATE TRIGGER worker_review_fail_publish BEFORE UPDATE OF status ON refresh_jobs WHEN NEW.status = 'done' BEGIN SELECT RAISE(ABORT, 'injected publication failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(ctx); err == nil || !strings.Contains(err.Error(), "injected publication failure") {
		t.Fatalf("publication error=%v", err)
	}
	if workerReviewRevision(t, db) != 2 || !reflect.DeepEqual(workerReviewVersion(t, db, "id"), before) {
		t.Fatal("publication failure leaked bank or revision")
	}
	if got := workerReviewJob(t, db, job.ID); got.Status != "failed" || !strings.Contains(got.LastError, "injected publication failure") {
		t.Fatalf("failure not recorded: %+v", got)
	}
}

func TestWorkerReviewMalformedJobsFailWithoutPublication(t *testing.T) {
	for _, languages := range []string{`[`, `[]`, `null`, `["unsupported"]`, `["../id"]`} {
		t.Run(languages, func(t *testing.T) {
			db, root := workerReviewDB(t), t.TempDir()
			workerReviewBank(t, root, "id", "valid")
			job := workerReviewEnqueue(t, db, 1)
			if err := db.Model(&RefreshJob{}).Where("id = ?", job.ID).Update("languages", languages).Error; err != nil {
				t.Fatal(err)
			}
			if err := workerReviewWorker(t, db, root).RunOnce(context.Background()); err == nil {
				t.Fatal("invalid job accepted")
			}
			if got := workerReviewJob(t, db, job.ID); got.Status != "failed" || got.Result != "" || got.LastError == "" {
				t.Fatalf("invalid job state: %+v", got)
			}
			if workerReviewRevision(t, db) != 1 {
				t.Fatal("invalid job published revision")
			}
		})
	}
}

func TestWorkerReviewPublicRoutesNeverQueryDatabaseAfterPublication(t *testing.T) {
	rt := testRuntime(t)
	root := t.TempDir()
	workerReviewBank(t, root, "id", "publicsnapshot")
	workerReviewEnqueue(t, rt.db, 1)
	if err := workerReviewWorker(t, rt.db, root).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := rt.ReloadIfChanged(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := NewRouterWithRuntime(rt, RouterOptions{AssetRoot: "../../assets", Admin: &AdminOptions{}})
	var queries atomic.Int64
	count := func(*gorm.DB) { queries.Add(1) }
	// Count every GORM execution family, including raw SQL/Row and writes.
	for _, err := range []error{
		rt.db.Callback().Query().Before("gorm:query").Register("worker_review_queries", count),
		rt.db.Callback().Raw().Before("gorm:raw").Register("worker_review_raw", count),
		rt.db.Callback().Row().Before("gorm:row").Register("worker_review_row", count),
		rt.db.Callback().Create().Before("gorm:create").Register("worker_review_create", count),
		rt.db.Callback().Update().Before("gorm:update").Register("worker_review_update", count),
		rt.db.Callback().Delete().Before("gorm:delete").Register("worker_review_delete", count),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"/", "/apps", "/apps/unknown?lan=id", "/category/game/", "/arbitrary/deep", "/robots.txt", "/sitemap.xml", "/__preview/", "/__preview/apps/sample-app-one"}
	fetch := func(path, method, ua string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Host = "public.example.test"
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	baseline := map[string]string{}
	for _, path := range paths {
		w := fetch(path, "GET", "Mozilla/5.0")
		if w.Code != 200 {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		baseline[path] = w.Body.String()
	}
	var wg sync.WaitGroup
	for _, path := range paths {
		for _, ua := range []string{"Mozilla/5.0", "Googlebot"} {
			wg.Add(1)
			go func(path, ua string) {
				defer wg.Done()
				w := fetch(path, "GET", ua)
				if w.Code != 200 || w.Body.String() != baseline[path] {
					t.Errorf("concurrent public response changed: %s %s", path, ua)
				}
			}(path, ua)
		}
	}
	wg.Wait()
	if got := queries.Load(); got != 0 {
		t.Fatalf("public requests issued %d database operations", got)
	}
	conn, err := rt.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		w := fetch(path, "GET", "Googlebot")
		if w.Code != 200 || w.Body.String() != baseline[path] {
			t.Errorf("DB outage changed public response: %s", path)
		}
		if !strings.HasPrefix(path, "/__preview") {
			h := fetch(path, "HEAD", "Googlebot")
			if h.Code != 200 || h.Body.Len() != 0 {
				t.Errorf("HEAD %s = %d / %d bytes", path, h.Code, h.Body.Len())
			}
			if h.Header().Get("X-Runtime-Revision") != fmt.Sprint(rt.Snapshot().Revision) {
				t.Errorf("HEAD %s lost revision", path)
			}
		}
	}
	if got := queries.Load(); got != 0 {
		t.Fatalf("outage public requests attempted %d database operations", got)
	}
}
