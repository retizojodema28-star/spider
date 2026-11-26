package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RefreshJob struct {
	ID          string     `gorm:"primaryKey;size:64" json:"job_id"`
	Languages   string     `gorm:"type:text;not null" json:"languages"`
	Status      string     `gorm:"size:20;index;not null" json:"status"`
	Phase       string     `gorm:"size:40" json:"phase"`
	Attempts    int        `gorm:"not null" json:"attempts"`
	MaxAttempts int        `gorm:"not null" json:"max_attempts"`
	AvailableAt time.Time  `gorm:"index;not null" json:"available_at"`
	LeaseOwner  string     `gorm:"size:64" json:"-"`
	LeaseUntil  *time.Time `gorm:"index" json:"lease_until"`
	LastError   string     `gorm:"type:text" json:"last_error"`
	Result      string     `gorm:"type:text" json:"result"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type WorkerHeartbeat struct {
	ID     string    `gorm:"primaryKey;size:64" json:"id"`
	SeenAt time.Time `gorm:"not null;index" json:"seen_at"`
	JobID  string    `gorm:"size:64" json:"job_id"`
}

type WorkerOptions struct {
	LanguageRoot  string
	Languages     []string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
	MaxAttempts   int
	OnError       func(error)
}

type Worker struct {
	db       *gorm.DB
	opts     WorkerOptions
	id       string
	lastTick atomic.Int64
}

var ErrRefreshBusy = errors.New("a refresh is already queued or running")
var ErrLeaseLost = errors.New("refresh lease lost")

func NewWorker(db *gorm.DB, opts WorkerOptions) (*Worker, error) {
	if db == nil {
		return nil, fmt.Errorf("worker database is required")
	}
	var err error
	opts.Languages, err = runtimeLanguages(opts.Languages)
	if err != nil {
		return nil, err
	}
	if opts.LanguageRoot == "" {
		opts.LanguageRoot = "data/lan"
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 60 * time.Second
	}
	if opts.LeaseDuration < 300*time.Millisecond {
		return nil, fmt.Errorf("lease duration must be at least 300ms")
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 5 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	id, err := adminToken()
	if err != nil {
		return nil, err
	}
	return &Worker{db: db, opts: opts, id: id}, nil
}

// EnqueueRefresh serializes admission through the revision row. Force never
// steals a live lease; operators can retry terminal failures explicitly.
func EnqueueRefresh(ctx context.Context, db *gorm.DB, languages []string, maxAttempts int) (RefreshJob, error) {
	langs, err := runtimeLanguages(languages)
	if err != nil {
		return RefreshJob{}, err
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if maxAttempts > 10 {
		return RefreshJob{}, fmt.Errorf("max_attempts must be 1..10")
	}
	id, err := adminToken()
	if err != nil {
		return RefreshJob{}, err
	}
	encoded, _ := json.Marshal(langs)
	job := RefreshJob{ID: id, Languages: string(encoded), Status: "queued", Phase: "waiting", MaxAttempts: maxAttempts, AvailableAt: time.Now().UTC()}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rev RuntimeRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, 1).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&RefreshJob{}).Where("status IN ?", []string{"queued", "running", "retry"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRefreshBusy
		}
		return tx.Create(&job).Error
	})
	return job, err
}

func (w *Worker) heartbeat(ctx context.Context, job string) error {
	now := time.Now().UTC()
	err := w.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"seen_at", "job_id"})}).Create(&WorkerHeartbeat{ID: w.id, SeenAt: now, JobID: job}).Error
	if err == nil {
		w.lastTick.Store(now.UnixNano())
	}
	return err
}

func (w *Worker) Health(ctx context.Context) error {
	last := w.lastTick.Load()
	if last == 0 || time.Since(time.Unix(0, last)) > max(3*w.opts.PollInterval, w.opts.LeaseDuration) {
		return fmt.Errorf("worker heartbeat is stale")
	}
	db, err := w.db.DB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

func (w *Worker) claim(ctx context.Context) (*RefreshJob, error) {
	now := time.Now().UTC()
	var job RefreshJob
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Commit expiry maintenance even when there is no claimable next job.
		var exhausted []RefreshJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status = ? AND lease_until < ? AND attempts >= max_attempts", "running", now).Find(&exhausted).Error; err != nil {
			return err
		}
		for _, expired := range exhausted {
			if err := tx.Model(&RefreshJob{}).Where("id = ?", expired.ID).Updates(map[string]any{"status": "failed", "phase": "finished", "last_error": "worker lease expired after final attempt", "lease_owner": "", "lease_until": nil, "finished_at": now}).Error; err != nil {
				return err
			}
			if err := recordRefreshEvent(tx, expired, "failed", "worker lease expired after final attempt"); err != nil {
				return err
			}
		}
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("((status IN ? AND available_at <= ?) OR (status = ? AND lease_until < ?)) AND attempts < max_attempts", []string{"queued", "retry"}, now, "running", now).Order("created_at asc").Limit(1)
		if err := q.First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		until := now.Add(w.opts.LeaseDuration)
		patch := map[string]any{"status": "running", "phase": "read_local_files", "attempts": gorm.Expr("attempts + 1"), "lease_owner": w.id, "lease_until": until, "started_at": now, "finished_at": nil}
		if job.Status == "running" {
			patch["last_error"] = "recovered expired worker lease"
		}
		res := tx.Model(&RefreshJob{}).Where("id = ? AND attempts = ? AND status = ?", job.ID, job.Attempts, job.Status).Updates(patch)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrLeaseLost
		}
		if job.Status == "running" {
			if err := recordRefreshEvent(tx, job, "lease_expired", "recovered expired worker lease"); err != nil {
				return err
			}
		}
		job.Attempts++
		job.LeaseOwner = w.id
		job.LeaseUntil = &until
		job.Status = "running"
		return recordRefreshEvent(tx, job, "running", "reading local language files")
	})
	if err != nil {
		return nil, err
	}
	if job.ID == "" {
		return nil, nil
	}
	return &job, nil
}

func (w *Worker) renew(ctx context.Context, job *RefreshJob) error {
	now := time.Now().UTC()
	res := w.db.WithContext(ctx).Model(&RefreshJob{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > ?", job.ID, "running", w.id, now).Update("lease_until", now.Add(w.opts.LeaseDuration))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return w.heartbeat(ctx, job.ID)
}

func (w *Worker) finish(ctx context.Context, job *RefreshJob, banks map[string]languageData) error {
	return UpdateRuntime(ctx, w.db, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		stats := map[string]any{}
		for lang, data := range banks {
			payload, err := json.Marshal(runtimeBankJSON{Words: data.words, Sentences: data.sentences})
			if err != nil {
				return err
			}
			v := LanguageVersion{Language: lang, JobID: job.ID, Payload: string(payload)}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "language"}}, DoUpdates: clause.AssignmentColumns([]string{"job_id", "payload", "updated_at"})}).Create(&v).Error; err != nil {
				return err
			}
			stats[lang] = map[string]int{"words": len(data.words), "sentences": len(data.sentences)}
		}
		result, _ := json.Marshal(stats)
		now = time.Now().UTC()
		res := tx.Model(&RefreshJob{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > ?", job.ID, "running", w.id, now).Updates(map[string]any{"status": "done", "phase": "finished", "result": string(result), "last_error": "", "finished_at": now, "lease_owner": "", "lease_until": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrLeaseLost
		}
		return recordRefreshEvent(tx, *job, "done", "validated language bank published")
	})
}

func (w *Worker) fail(ctx context.Context, job *RefreshJob, cause error) error {
	now := time.Now().UTC()
	status := "retry"
	var finished *time.Time
	if job.Attempts >= job.MaxAttempts {
		status = "failed"
		finished = &now
	}
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&RefreshJob{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > ?", job.ID, "running", w.id, now).Updates(map[string]any{"status": status, "phase": "finished", "last_error": message, "available_at": now.Add(w.opts.RetryDelay * time.Duration(1<<min(job.Attempts-1, 6))), "lease_owner": "", "lease_until": nil, "finished_at": finished})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrLeaseLost
		}
		return recordRefreshEvent(tx, *job, status, message)
	})
}

// RunOnce is useful for one-shot jobs and deterministic integration tests.
func (w *Worker) RunOnce(ctx context.Context) error {
	if err := w.heartbeat(ctx, ""); err != nil {
		return err
	}
	if err := CleanupAdminSessions(ctx, w.db, 200); err != nil {
		return err
	}
	job, err := w.claim(ctx)
	if err != nil || job == nil {
		return err
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	renewed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(w.opts.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				renewed <- nil
				return
			case <-workCtx.Done():
				renewed <- workCtx.Err()
				return
			case <-ticker.C:
				if err := w.renew(workCtx, job); err != nil {
					cancel()
					renewed <- err
					return
				}
			}
		}
	}()
	var langs []string
	err = json.Unmarshal([]byte(job.Languages), &langs)
	banks := map[string]languageData{}
	if err == nil {
		for _, lang := range langs {
			allowed := false
			for _, v := range w.opts.Languages {
				if v == lang {
					allowed = true
				}
			}
			if !allowed {
				err = fmt.Errorf("worker does not support language %s", lang)
				break
			}
			var data languageData
			data, err = readRuntimeLanguage(workCtx, w.opts.LanguageRoot, lang)
			if err != nil {
				break
			}
			banks[lang] = data
		}
	}
	if err == nil && len(banks) == 0 {
		err = fmt.Errorf("refresh job contains no languages")
	}
	if err == nil {
		err = w.finish(workCtx, job, banks)
	}
	close(stop)
	renewErr := <-renewed
	// A heartbeat racing a successful terminal transition may see a lost
	// lease; completion already fenced and committed atomically.
	if err != nil && renewErr != nil && !errors.Is(renewErr, context.Canceled) {
		err = renewErr
	}
	if err != nil {
		// A graceful shutdown records retry state; a hard crash is recovered by lease.
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if failErr := w.fail(cleanup, job, err); failErr != nil && !errors.Is(failErr, ErrLeaseLost) {
			return fmt.Errorf("refresh failed: %v; record failure: %w", err, failErr)
		}
		return fmt.Errorf("refresh job %s attempt %d: %w", job.ID, job.Attempts, err)
	}
	return w.heartbeat(ctx, "")
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.db.WithContext(cleanup).Delete(&WorkerHeartbeat{}, "id = ?", w.id)
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil && w.opts.OnError != nil {
			w.opts.OnError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
