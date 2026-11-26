package site

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RuntimeHubState struct {
	ID                  uint   `gorm:"primaryKey"`
	ConfigJSON          string `gorm:"type:text;not null"`
	ActiveDate          string `gorm:"size:10"`
	MissingDateStrategy string `gorm:"size:20;not null;default:latest"`
	NextExternalID      uint   `gorm:"not null"`
	UpdatedAt           time.Time
}
type RuntimeHubSubdomain struct {
	Domain    string `gorm:"primaryKey;size:255"`
	Prefixes  string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}
type RuntimeExternalDate struct {
	Date      string `gorm:"primaryKey;size:10"`
	MaxID     uint   `gorm:"not null"`
	UpdatedAt time.Time
}

// Snapshot IDs are independent of SiteLink IDs. The loader tolerates holes in
// legacy/direct data, while the admin delete endpoint rebuilds the whole pool.
type RuntimeExternalRecord struct {
	ID            uint   `gorm:"primaryKey;autoIncrement:false"`
	LinkID        uint   `gorm:"index;not null"` // provenance only; duplicates are valid
	FirstSeenDate string `gorm:"size:10;index"`
	Payload       string `gorm:"type:text"` // immutable normalized PHP import row
}

// Older releases used a unique liveness join to SiteLink. Remove that one index
// before AutoMigrate creates the nonunique provenance index (also on SQLite).
func migrateRuntimeExternalIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&RuntimeExternalRecord{}) {
		return nil
	}
	indexes, err := db.Migrator().GetIndexes(&RuntimeExternalRecord{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, _ := index.Unique()
		cols := index.Columns()
		if unique && len(cols) == 1 && cols[0] == "link_id" {
			if err := db.Migrator().DropIndex(&RuntimeExternalRecord{}, index.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadRuntimeHub(tx *gorm.DB, links []SiteLink) (HubSnapshot, error) {
	var state RuntimeHubState
	var domains []RuntimeHubSubdomain
	var dates []RuntimeExternalDate
	var records []RuntimeExternalRecord
	for _, q := range []*gorm.DB{tx.First(&state, 1), tx.Order("domain asc").Find(&domains), tx.Order("date asc").Find(&dates), tx.Order("id asc").Find(&records)} {
		if q.Error != nil {
			return HubSnapshot{}, q.Error
		}
	}
	cfg := DecodeHubConfig([]byte(state.ConfigJSON))
	hub := HubSnapshot{Config: &cfg, ActiveDate: state.ActiveDate, MissingDateStrategy: state.MissingDateStrategy, Subdomains: map[string][]string{}, SnapshotMaxIDs: map[string]int{}, ExternalRecords: map[int]HubExternalRecord{}}
	for _, d := range domains {
		var prefixes []string
		if err := json.Unmarshal([]byte(d.Prefixes), &prefixes); err != nil {
			return HubSnapshot{}, fmt.Errorf("invalid persisted hub subdomains")
		}
		hub.Subdomains[d.Domain] = prefixes
	}
	for _, d := range dates {
		hub.SnapshotMaxIDs[d.Date] = int(d.MaxID)
	}
	byID := map[uint]SiteLink{}
	for _, l := range links {
		byID[l.ID] = l
	}
	for _, rec := range records {
		if rec.Payload != "" {
			var row HubExternalRecord
			if err := json.Unmarshal([]byte(rec.Payload), &row); err != nil {
				return HubSnapshot{}, fmt.Errorf("invalid persisted external snapshot %d", rec.ID)
			}
			row.ID = int(rec.ID)
			hub.ExternalRecords[int(rec.ID)] = row
			continue
		}
		// Compatibility for directly loaded legacy sparse records. Normal admin
		// writes always store their own payload, never a live join.
		l, ok := byID[rec.LinkID]
		if !ok || l.Kind != "external" {
			continue
		}
		hub.ExternalRecords[int(rec.ID)] = HubExternalRecord{ID: int(rec.ID), Domain: l.Domain, AnchorText: l.AnchorText, TargetURL: l.TargetURL, Weight: l.Weight, Enabled: l.Enabled}
	}
	return hub, nil
}

func appendRuntimeExternal(tx *gorm.DB, links []SiteLink, date, source string) error {
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	if source == "" {
		source = "admin_stats_textarea"
	}
	var state RuntimeHubState
	if err := tx.First(&state, 1).Error; err != nil {
		return err
	}
	rows := make([]RuntimeExternalRecord, 0, len(links))
	for _, l := range links {
		state.NextExternalID++
		rows = append(rows, RuntimeExternalRecord{ID: state.NextExternalID, LinkID: l.ID, FirstSeenDate: date, Payload: runtimePHPLinkJSON(l, source, state.NextExternalID)})
	}
	state.ActiveDate = date
	if err := tx.Save(&state).Error; err != nil {
		return err
	}
	if len(rows) != 0 {
		if err := tx.CreateInBatches(&rows, 100).Error; err != nil {
			return err
		}
	}
	var boundary RuntimeExternalDate
	if err := tx.Where("date = ?", date).Limit(1).Find(&boundary).Error; err != nil {
		return err
	}
	if len(rows) != 0 {
		boundary.MaxID = max(boundary.MaxID, state.NextExternalID)
	}
	boundary.Date = date
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "date"}}, DoUpdates: clause.AssignmentColumns([]string{"max_id", "updated_at"})}).Create(&boundary).Error
}

func clearRuntimeExternal(tx *gorm.DB) error {
	for _, model := range []any{&RuntimeExternalDate{}, &RuntimeExternalRecord{}} {
		if err := tx.Where("1 = 1").Delete(model).Error; err != nil {
			return err
		}
	}
	return tx.Model(&RuntimeHubState{}).Where("id = ?", 1).Updates(map[string]any{"next_external_id": 0, "active_date": ""}).Error
}

// PHP AdminLinkConfigRepository::deleteExternalLinks reindexes ordinary rows,
// then clears every historical snapshot and imports the survivors under today.
// A delete of nonexistent IDs still rebuilds; deleting the final row clears all.
func rebuildRuntimeExternal(tx *gorm.DB) error {
	if err := clearRuntimeExternal(tx); err != nil {
		return err
	}
	var links []SiteLink
	if err := tx.Where("site_key = ? AND kind = ?", "default", "external").Order("id asc").Find(&links).Error; err != nil {
		return err
	}
	date := time.Now().UTC().Format("2006-01-02")
	// Preserve each survivor's source_file, but snapshot its current payload at
	// today's date. Batch append order is ordinary stored order, including disabled.
	var metadata []RuntimeLinkMetadata
	if err := tx.Find(&metadata).Error; err != nil {
		return err
	}
	byID := map[uint]string{}
	for _, m := range metadata {
		byID[m.LinkID] = m.SourceFile
	}
	for _, l := range links {
		if err := appendRuntimeExternal(tx, []SiteLink{l}, date, byID[l.ID]); err != nil {
			return err
		}
	}
	return nil
}

func backfillRuntimeExternalPayloads(tx *gorm.DB) (int, error) {
	var records []RuntimeExternalRecord
	if err := tx.Where("payload IS NULL OR payload = ?", "").Find(&records).Error; err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	var links []SiteLink
	var metadata []RuntimeLinkMetadata
	var dates []RuntimeExternalDate
	for _, q := range []*gorm.DB{tx.Find(&links), tx.Find(&metadata), tx.Order("date asc").Find(&dates)} {
		if q.Error != nil {
			return 0, q.Error
		}
	}
	byID := map[uint]SiteLink{}
	meta := map[uint]RuntimeLinkMetadata{}
	for _, l := range links {
		byID[l.ID] = l
	}
	for _, m := range metadata {
		meta[m.LinkID] = m
	}
	count := 0
	for _, rec := range records {
		l, ok := byID[rec.LinkID]
		if !ok || l.SiteKey != "default" || l.Kind != "external" {
			continue // a preexisting hole cannot be reconstructed from missing data
		}
		m := meta[l.ID]
		date := m.EffectiveDate
		if date == "" {
			for _, d := range dates {
				if d.MaxID >= rec.ID {
					date = d.Date
					break
				}
			}
		}
		if err := tx.Model(&RuntimeExternalRecord{}).Where("id = ?", rec.ID).Updates(map[string]any{"first_seen_date": date, "payload": runtimePHPLinkJSON(l, m.SourceFile, rec.ID)}).Error; err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}
