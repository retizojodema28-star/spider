package site

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestRuntimePHPIDMigrationAndIndependentPayload(t *testing.T) {
	h := newAdminHarness(t, nil)
	verifyRuntimePHPIDMigration(t, h)
}

func TestRuntimeHubBackfillsLegacySnapshotRecommendTier(t *testing.T) {
	h := newAdminHarness(t, nil)
	link := SiteLink{SiteKey: "default", Kind: "external", Language: "id", Domain: "tier-two.test", AnchorText: "Tier Two", TargetURL: "https://tier-two.test/", RecommendTier: 2, Weight: 100, Enabled: true}
	if err := h.db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	payload := `{"domain":"tier-two.test","lang":"id","anchor_text":"Tier Two","target_url":"https://tier-two.test/","category":"","weight":100,"enabled":true,"remark":""}`
	if err := h.db.Create(&RuntimeExternalRecord{ID: 1, LinkID: link.ID, FirstSeenDate: "2026-11-26", Payload: payload}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&RuntimeExternalDate{Date: "2026-11-26", MaxID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&RuntimeHubState{}).Where("id = ?", 1).Updates(map[string]any{"active_date": "2026-11-26", "next_external_id": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	hub := h.rt.Snapshot().Hub
	if got := hub.ExternalRecords[1].RecommendTier; got != 2 {
		t.Fatalf("legacy snapshot tier = %d, want 2", got)
	}
	if got := hub.TierCounts["2026-11-26"][2]; got != 1 {
		t.Fatalf("tier count = %d, want 1", got)
	}
}

// Also called by TestAdminOptionalMySQL against its isolated prefixed tables.
func verifyRuntimePHPIDMigration(t *testing.T, h *adminHarness) {
	t.Helper()
	links := []SiteLink{
		{ID: 90, SiteKey: "preview", Kind: "external", Domain: "preview.test", Language: "id", AnchorText: "Preview", TargetURL: "https://preview.test/", Enabled: true},
		{ID: 100, SiteKey: "default", Kind: "external", Domain: "one.test", Language: "id", AnchorText: "First", TargetURL: "https://one.test/", Weight: 20, Enabled: true},
		{ID: 120, SiteKey: "default", Kind: "main-external", Domain: "main.test", Language: "id", AnchorText: "Main", TargetURL: "https://main.test/", Enabled: true},
		{ID: 130, SiteKey: "default", Kind: "external", Domain: "two.test", Language: "id", AnchorText: "Second", TargetURL: "https://two.test/", Weight: 900, Enabled: true},
	}
	for i := range links {
		if err := h.db.Create(&links[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	meta := []RuntimeLinkMetadata{
		{LinkID: 100, EffectiveDate: "2026-05-14", SourceFile: "historical.csv", PHPJSON: runtimePHPLinkJSON(links[1], "historical.csv", 100)},
		{LinkID: 120, PHPID: 17, SourceFile: "main.csv", PHPJSON: runtimePHPLinkJSON(links[2], "main.csv", 17)},
	}
	if err := h.db.Create(&meta).Error; err != nil {
		t.Fatal(err)
	}
	domains := []InternalDomain{{ID: 70, SiteKey: "preview", Domain: "preview.test", Path: "/", Language: "id"}, {ID: 80, SiteKey: "default", Domain: "default.test", Path: "/", Language: "id"}}
	if err := h.db.Create(&domains).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&RuntimeExternalRecord{ID: 1, LinkID: 100}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&RuntimeExternalDate{Date: "2026-05-14", MaxID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&RuntimeHubState{}).Where("id = 1").Updates(map[string]any{"active_date": "2026-05-14", "next_external_id": 1}).Error; err != nil {
		t.Fatal(err)
	}
	// Reproduce the pre-upgrade uniqueness constraint on provenance.
	stmt := &gorm.Statement{DB: h.db}
	if err := stmt.Parse(&RuntimeExternalRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("CREATE UNIQUE INDEX ? ON ? (?)", clause.Column{Name: "old_snapshot_link_unique"}, clause.Table{Name: stmt.Table}, clause.Column{Name: "link_id"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateAdmin(h.db); err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := h.rt.Snapshot()
	for sqlID, want := range map[uint]uint{90: 1, 100: 1, 120: 17, 130: 2} {
		m := s.LinkMetadata[sqlID]
		var row struct{ ID uint }
		if err := json.Unmarshal([]byte(m.PHPJSON), &row); err != nil || m.PHPID != want || row.ID != want {
			t.Fatalf("migrate SQL ID %d: metadata %+v JSON ID %d error %v", sqlID, m, row.ID, err)
		}
	}
	if m := s.LinkMetadata[100]; m.EffectiveDate != "2026-05-14" || m.SourceFile != "historical.csv" {
		t.Fatal("migration changed source metadata")
	}
	for _, d := range s.InternalDomains {
		if d.PHPID != 1 {
			t.Fatalf("internal-domain migration crossed namespaces: %+v", d)
		}
	}
	var original RuntimeExternalRecord
	if err := h.db.First(&original, 1).Error; err != nil {
		t.Fatal(err)
	}
	if original.FirstSeenDate != "2026-05-14" || original.Payload == "" || s.Hub.ExternalRecords[1].AnchorText != "First" {
		t.Fatalf("legacy snapshot payload was not frozen: %+v", original)
	}
	if err := MigrateAdmin(h.db); err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Revision != h.rt.Snapshot().Revision || !reflect.DeepEqual(s.LinkMetadata, h.rt.Snapshot().LinkMetadata) {
		t.Fatal("repeated migration rewrote assigned logical IDs or row metadata")
	}
	if err := UpdateRuntime(context.Background(), h.db, func(tx *gorm.DB) error {
		if err := tx.Model(&SiteLink{}).Where("id = 100").Update("anchor_text", "Edited ordinary row").Error; err != nil {
			return err
		}
		duplicate := original
		duplicate.ID = 2
		if err := tx.Create(&duplicate).Error; err != nil {
			return err // the pre-upgrade UNIQUE LinkID must have been removed
		}
		return tx.Model(&RuntimeHubState{}).Where("id = 1").Update("next_external_id", 2).Error
	}); err != nil {
		t.Fatal(err)
	}
	restart, err := NewRuntime(context.Background(), h.db, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		if row := restart.Snapshot().Hub.ExternalRecords[id]; row.AnchorText != "First" || row.ID != id {
			t.Fatalf("snapshot %d was rewritten through its ordinary-row provenance: %+v", id, row)
		}
	}
}

func TestAdminMainAppendUsesMaximumLogicalIDAndPreservesRows(t *testing.T) {
	h := newAdminHarness(t, nil)
	h.login()
	h.call("POST", "/admin/api/main-page-external-links/import", map[string]any{"strategy": "replace", "source_file": "first.csv", "links": "https://one.test/A,One\nhttps://two.test/B,Two"}, 200)
	var rows []SiteLink
	if err := h.db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := UpdateRuntime(context.Background(), h.db, func(tx *gorm.DB) error {
		for i, l := range rows {
			if err := saveRuntimeLinkMetadata(tx, l, "", "first.csv", uint(7+i*5)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := h.rt.Snapshot()
	h.call("POST", "/admin/api/main-page-external-links/import", map[string]any{"strategy": "append", "source_file": "next.csv", "links": "https://one.test/a,ONE\nhttps://three.test/,Three"}, 200)
	s := h.rt.Snapshot()
	if len(s.Links) != 3 {
		t.Fatal("case-folded duplicate consumed an ordinary ID")
	}
	for _, l := range rows {
		if s.LinkMetadata[l.ID] != old.LinkMetadata[l.ID] {
			t.Fatal("main append changed existing logical IDs, timestamps or ordered JSON")
		}
	}
	var added SiteLink
	for _, l := range s.Links {
		if l.TargetURL == "https://three.test/" {
			added = l
		}
	}
	if m := s.LinkMetadata[added.ID]; m.PHPID != 13 || m.SourceFile != "next.csv" {
		t.Fatalf("main append did not use max logical ID: %+v", m)
	}
	h.call("DELETE", "/admin/api/main-page-external-links", map[string]any{"id": rows[0].ID}, 200)
	if s := h.rt.Snapshot(); s.LinkMetadata[rows[1].ID].PHPID != 1 || s.LinkMetadata[added.ID].PHPID != 2 {
		t.Fatal("delete did not compact logical IDs while preserving SQL handles")
	}
	added.AnchorText = "Edited"
	h.call("PUT", "/admin/api/main-page-external-links", adminLinkJSON(added), 200)
	if m := h.rt.Snapshot().LinkMetadata[added.ID]; m.PHPID != 2 || m.SourceFile != "next.csv" {
		t.Fatal("CRUD edit restored SQL ID or lost source metadata")
	}
}
