package site

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RuntimeRevision is the single serialization point for configuration and bank publication.
type RuntimeRevision struct {
	ID        uint   `gorm:"primaryKey"`
	Revision  uint64 `gorm:"not null"`
	UpdatedAt time.Time
}

// LanguageVersion contains the active, validated bank, so worker and web processes
// do not need a shared writable filesystem or persistent volume.
type LanguageVersion struct {
	Language  string `gorm:"primaryKey;size:12"`
	JobID     string `gorm:"size:64;not null"`
	Payload   string `gorm:"type:longtext;not null"`
	UpdatedAt time.Time
}

// InternalDomain is the PHP internal-domains collection, not an app-local link.
type InternalDomain struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	PHPID     uint      `gorm:"not null;default:0" json:"php_id"`
	SiteKey   string    `gorm:"size:120;index;not null" json:"site_key"`
	Domain    string    `gorm:"size:255;not null" json:"domain"`
	Title     string    `gorm:"size:255;not null" json:"title"`
	Path      string    `gorm:"size:1000;not null" json:"path"`
	Language  string    `gorm:"size:12;not null" json:"lang"`
	Category  string    `gorm:"size:100" json:"category"`
	Weight    int       `gorm:"not null" json:"weight"`
	Enabled   bool      `gorm:"not null" json:"enabled"`
	Remark    string    `gorm:"size:500" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MigrateAdmin supplements Migrate; the parent must call both at startup.
func MigrateAdmin(db *gorm.DB) error {
	if err := migrateRuntimeExternalIndexes(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(&AdminUser{}, &AdminSession{}, &RuntimeRevision{}, &LanguageVersion{}, &InternalDomain{}, &RefreshJob{}, &RefreshJobEvent{}, &WorkerHeartbeat{}, &RuntimeHubState{}, &RuntimeHubSubdomain{}, &RuntimeExternalDate{}, &RuntimeExternalRecord{}, &RuntimeLinkMetadata{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&RuntimeRevision{ID: 1, Revision: 1}).Error; err != nil {
		return err
	}
	encoded, _ := json.Marshal(DefaultHubConfig())
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&RuntimeHubState{ID: 1, ConfigJSON: string(encoded), MissingDateStrategy: "latest"}).Error; err != nil {
		return err
	}
	return migrateRuntimePHPIDs(db)
}

// UpdateRuntime commits a mutation and revision together. All parent-side writes
// to public data (including seed/app imports after startup) must use this helper.
func UpdateRuntime(ctx context.Context, db *gorm.DB, change func(*gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&RuntimeRevision{}).Where("id = ?", 1).Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return change(tx)
	})
}
