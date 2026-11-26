package site

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RuntimeLinkMetadata keeps PHP import metadata without changing the shared
// SiteLink model. Snapshot.LinkMetadata is keyed by SiteLink.ID.
type RuntimeLinkMetadata struct {
	LinkID        uint      `gorm:"primaryKey;autoIncrement:false" json:"link_id"`
	PHPID         uint      `gorm:"not null;default:0" json:"php_id"`
	EffectiveDate string    `gorm:"size:10;index" json:"effective_date"`
	SourceFile    string    `gorm:"size:160" json:"source_file"`
	PHPJSON       string    `gorm:"type:text" json:"-"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func runtimePHPLinkJSON(l SiteLink, source string, phpID uint) string {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if l.Kind == "internal" {
		_ = encoder.Encode(struct {
			URL   string `json:"url"`
			Label string `json:"label"`
			ID    uint   `json:"id"`
		}{l.TargetURL, l.AnchorText, phpID})
	} else {
		_ = encoder.Encode(struct {
			Domain   string `json:"domain"`
			Language string `json:"lang"`
			Anchor   string `json:"anchor_text"`
			URL      string `json:"target_url"`
			Category string `json:"category"`
			Weight   int    `json:"weight"`
			Enabled  bool   `json:"enabled"`
			Remark   string `json:"remark"`
			Tier     int    `json:"recommend_tier"`
			URLAlias string `json:"url"`
			Label    string `json:"label"`
			Source   string `json:"source_file"`
			ID       uint   `json:"id"`
		}{l.Domain, l.Language, l.AnchorText, l.TargetURL, l.Category, l.Weight, l.Enabled, l.Remark, l.RecommendTier, l.TargetURL, l.AnchorText, source, phpID})
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func saveRuntimeLinkMetadata(tx *gorm.DB, l SiteLink, date, source string, phpID uint) error {
	if source == "" {
		source = "admin_stats_textarea"
	}
	m := RuntimeLinkMetadata{LinkID: l.ID, PHPID: phpID, EffectiveDate: date, SourceFile: source, PHPJSON: runtimePHPLinkJSON(l, source, phpID)}
	return writeRuntimeLinkMetadata(tx, []RuntimeLinkMetadata{m})
}

func writeRuntimeLinkMetadata(tx *gorm.DB, rows []RuntimeLinkMetadata) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "link_id"}}, DoUpdates: clause.AssignmentColumns([]string{"php_id", "effective_date", "source_file", "php_json", "updated_at"})}).CreateInBatches(&rows, 100).Error
}

// reindexRuntimeLinks runs under UpdateRuntime's revision lock. SQL primary keys
// are stable API handles, never PHP row numbers. PHP mergeLinks/reindexRows use
// insertion order, not the weighted admin display order. Main-page append alone
// preserves every existing JSON row and assigns max(existing PHPID)+1 to new rows.
// This deliberately does not touch RuntimeExternalRecord's dated snapshot IDs.
func reindexRuntimeLinks(tx *gorm.DB, siteKey, kind string, preserveExisting bool) error {
	var links []SiteLink
	if err := tx.Where("site_key = ? AND kind = ?", siteKey, kind).Order("id asc").Find(&links).Error; err != nil {
		return err
	}
	var metadata []RuntimeLinkMetadata
	if err := tx.Where("link_id IN (?)", tx.Model(&SiteLink{}).Select("id").Where("site_key = ? AND kind = ?", siteKey, kind)).Find(&metadata).Error; err != nil {
		return err
	}
	byID := make(map[uint]RuntimeLinkMetadata, len(metadata))
	var maxID uint
	for _, m := range metadata {
		byID[m.LinkID] = m
		maxID = max(maxID, m.PHPID)
	}
	changed := []RuntimeLinkMetadata{}
	for i, l := range links {
		old := byID[l.ID]
		if preserveExisting && old.PHPID != 0 && old.PHPJSON != "" {
			continue
		}
		m := old
		m.LinkID = l.ID
		if preserveExisting {
			if m.PHPID == 0 {
				maxID++
				m.PHPID = maxID
			}
		} else {
			m.PHPID = uint(i + 1)
		}
		if m.SourceFile == "" {
			m.SourceFile = "admin_stats_textarea"
		}
		m.PHPJSON = runtimePHPLinkJSON(l, m.SourceFile, m.PHPID)
		if m != old {
			m.UpdatedAt = time.Now().UTC()
			changed = append(changed, m)
		}
	}
	return writeRuntimeLinkMetadata(tx, changed)
}

func reindexRuntimeInternalDomains(tx *gorm.DB, siteKey string) error {
	var domains []InternalDomain
	if err := tx.Where("site_key = ?", siteKey).Order("id asc").Find(&domains).Error; err != nil {
		return err
	}
	for i, d := range domains {
		if d.PHPID != uint(i+1) {
			if err := tx.Model(&InternalDomain{}).Where("id = ?", d.ID).Update("php_id", i+1).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// Backfill older installations once without changing their SQL/API identifiers,
// date boundaries, source fields, or already assigned main-page logical IDs.
// The worker can migrate its own schema before the web's SiteLink table exists.
func migrateRuntimePHPIDs(db *gorm.DB) error {
	hasLinks := db.Migrator().HasTable(&SiteLink{})
	return db.Transaction(func(tx *gorm.DB) error {
		var rev RuntimeRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, 1).Error; err != nil {
			return err
		}
		var collections []struct{ SiteKey, Kind string }
		if hasLinks {
			assigned := tx.Model(&RuntimeLinkMetadata{}).Select("link_id").Where("php_id > 0")
			if err := tx.Model(&SiteLink{}).Distinct("site_key", "kind").Where("id NOT IN (?)", assigned).Find(&collections).Error; err != nil {
				return err
			}
		}
		var sites []string
		if err := tx.Model(&InternalDomain{}).Distinct("site_key").Where("php_id = 0").Pluck("site_key", &sites).Error; err != nil {
			return err
		}
		for _, c := range collections {
			if err := reindexRuntimeLinks(tx, c.SiteKey, c.Kind, c.Kind == "main-external"); err != nil {
				return err
			}
		}
		for _, site := range sites {
			if err := reindexRuntimeInternalDomains(tx, site); err != nil {
				return err
			}
		}
		backfilled := 0
		if hasLinks {
			var err error
			backfilled, err = backfillRuntimeExternalPayloads(tx)
			if err != nil {
				return err
			}
		}
		if len(collections)+len(sites)+backfilled == 0 {
			return nil
		}
		return tx.Model(&RuntimeRevision{}).Where("id = 1").Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()}).Error
	})
}

// LegacyManagedLinks adapts preserved PHP serialization for deterministic link
// sorting; callers should use this instead of rebuilding JSON from Go maps.
func (s *Snapshot) LegacyManagedLinks(kind string) []LegacyManagedLink {
	out := []LegacyManagedLink{}
	for _, l := range s.Links {
		if l.Kind != kind {
			continue
		}
		m := s.LinkMetadata[l.ID]
		id := m.PHPID
		if id == 0 {
			id = l.ID
		}
		out = append(out, LegacyManagedLink{ID: id, Domain: l.Domain, Language: l.Language, AnchorText: l.AnchorText, TargetURL: l.TargetURL, Category: l.Category, Weight: l.Weight, Enabled: l.Enabled, Remark: l.Remark, PHPJSON: json.RawMessage(m.PHPJSON)})
	}
	return out
}
