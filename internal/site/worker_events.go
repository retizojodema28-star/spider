package site

import (
	"time"

	"gorm.io/gorm"
)

// RefreshJobEvent retains diagnostics after a retry succeeds. At most 32 events
// per job are kept; status remains concise while errors stay queryable.
type RefreshJobEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	JobID     string    `gorm:"size:64;index;not null" json:"job_id"`
	Attempt   int       `gorm:"not null" json:"attempt"`
	Status    string    `gorm:"size:30;not null" json:"status"`
	Message   string    `gorm:"type:text" json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func recordRefreshEvent(tx *gorm.DB, job RefreshJob, status, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if err := tx.Create(&RefreshJobEvent{JobID: job.ID, Attempt: job.Attempts, Status: status, Message: message}).Error; err != nil {
		return err
	}
	var stale []uint
	if err := tx.Model(&RefreshJobEvent{}).Where("job_id = ?", job.ID).Order("id desc").Offset(32).Limit(100).Pluck("id", &stale).Error; err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	return tx.Where("id IN ?", stale).Delete(&RefreshJobEvent{}).Error
}
