package site

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *adminServer) enqueue(c *gin.Context) {
	var b struct {
		Languages   []string `json:"languages"`
		Force       bool     `json:"force"`
		MaxAttempts int      `json:"max_attempts"`
	}
	if !adminDecode(c, &b) {
		return
	}
	if len(b.Languages) == 0 {
		b.Languages = a.runtime.opts.Languages
	}
	for _, lang := range b.Languages {
		if err := a.runtime.validLanguage(lang); err != nil {
			adminError(c, 422, err.Error())
			return
		}
	}
	if b.MaxAttempts < 0 || b.MaxAttempts > 10 {
		adminError(c, 422, "max_attempts must be 1..10")
		return
	}
	job, err := EnqueueRefresh(c.Request.Context(), a.runtime.db, b.Languages, b.MaxAttempts)
	if err != nil {
		if errors.Is(err, ErrRefreshBusy) {
			adminError(c, 409, err.Error())
		} else {
			adminError(c, 503, "unable to queue refresh")
		}
		return
	}
	c.JSON(202, gin.H{"ok": true, "status": "queued", "job_id": job.ID, "job": job})
}

func (a *adminServer) jobs(c *gin.Context) {
	var jobs []RefreshJob
	q := a.runtime.db.WithContext(c.Request.Context()).Order("created_at desc").Limit(100)
	if id := c.Query("job_id"); id != "" {
		q = q.Where("id = ?", id)
	}
	if err := q.Find(&jobs).Error; err != nil {
		adminError(c, 503, "job storage unavailable")
		return
	}
	if c.Query("job_id") != "" && len(jobs) == 0 {
		adminError(c, 404, "job not found")
		return
	}
	var workers []WorkerHeartbeat
	if err := a.runtime.db.WithContext(c.Request.Context()).Where("seen_at > ?", time.Now().UTC().Add(-2*time.Minute)).Order("seen_at desc").Find(&workers).Error; err != nil {
		adminError(c, 503, "worker storage unavailable")
		return
	}
	ids := []string{}
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	var events []RefreshJobEvent
	if len(ids) > 0 {
		if err := a.runtime.db.WithContext(c.Request.Context()).Where("job_id IN ?", ids).Order("id desc").Limit(3200).Find(&events).Error; err != nil {
			adminError(c, 503, "job event storage unavailable")
			return
		}
	}
	adminOK(c, gin.H{"jobs": jobs, "events": events, "workers": workers, "revision": a.runtime.Snapshot().Revision})
}

func (a *adminServer) retry(c *gin.Context) {
	err := a.runtime.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var rev RuntimeRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, 1).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&RefreshJob{}).Where("status IN ?", []string{"queued", "running", "retry"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrRefreshBusy
		}
		res := tx.Model(&RefreshJob{}).Where("id = ? AND status = ?", c.Param("id"), "failed").Updates(map[string]any{"status": "queued", "phase": "waiting", "attempts": 0, "available_at": time.Now().UTC(), "finished_at": nil, "lease_owner": "", "lease_until": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRefreshBusy) {
			adminError(c, 409, err.Error())
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			adminError(c, 404, "failed job not found")
		} else {
			adminError(c, 503, "job storage unavailable")
		}
		return
	}
	adminOK(c, gin.H{"status": "queued", "job_id": c.Param("id")})
}
