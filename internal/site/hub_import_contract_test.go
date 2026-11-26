package site

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// This regression owns a separate temporary SQLite DB for every sequence. It
// goes through authenticated APIs, reload/restart, then the public router.
// Expected states are the real PHP operations documented in the live oracle in
// hub_import_oracle_test.go; no expected renderer IDs are copied from Go SQL IDs.
type hubImportOperation struct {
	Name, Action, Strategy, Date, Rows, DeleteURL string
	WantRows                                      []HubExternalRecord
	WantRecords                                   []HubExternalRecord
	WantDates                                     map[string]int
	WantActive                                    string
}
type hubImportSequence struct {
	Name       string
	Operations []hubImportOperation
}

func hubImportRow(letter string, weight int, enabled bool) HubExternalRecord {
	return HubExternalRecord{Domain: letter + ".hub-import.example", AnchorText: "Partner " + strings.ToUpper(letter), TargetURL: "https://" + letter + ".hub-import.example/path?source=fixture", Weight: weight, Enabled: enabled}
}
func hubImportText(rows ...HubExternalRecord) string {
	out := []string{}
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s|id|%s|%s|tools|%d|%t|source row", r.Domain, r.AnchorText, r.TargetURL, r.Weight, r.Enabled))
	}
	return strings.Join(out, "\n")
}
func hubImportIDs(rows ...HubExternalRecord) []HubExternalRecord {
	out := append([]HubExternalRecord{}, rows...)
	for i := range out {
		out[i].ID = i + 1
	}
	return out
}

func hubImportSequences(today string) []hubImportSequence {
	a, b, c, d, e := hubImportRow("a", 100, true), hubImportRow("b", 40, false), hubImportRow("c", 80, true), hubImportRow("d", 70, true), hubImportRow("e", 60, true)
	changedA := a
	changedA.Weight = 900
	changedA.Enabled = false
	date1, date2, date3 := "2026-05-14", "2026-05-16", "2026-05-17"
	return []hubImportSequence{
		{Name: "append_source_payloads_and_duplicate_ids", Operations: []hubImportOperation{
			{Name: "replace_three_with_disabled", Action: "import", Strategy: "replace", Date: date1, Rows: hubImportText(a, b, c), WantRows: hubImportIDs(a, b, c), WantRecords: hubImportIDs(a, b, c), WantDates: map[string]int{date1: 3}, WantActive: date1},
			{Name: "append_duplicate_changed_payload_and_within_batch_duplicate", Action: "import", Strategy: "append", Date: date2, Rows: hubImportText(changedA, a, d), WantRows: hubImportIDs(a, b, c, d), WantRecords: hubImportIDs(a, b, c, changedA, d), WantDates: map[string]int{date1: 3, date2: 5}, WantActive: date2},
			{Name: "append_only_existing_duplicate", Action: "import", Strategy: "append", Date: date3, Rows: hubImportText(a), WantRows: hubImportIDs(a, b, c, d), WantRecords: hubImportIDs(a, b, c, changedA, d, a), WantDates: map[string]int{date1: 3, date2: 5, date3: 6}, WantActive: date3},
			{Name: "nonexistent_delete_still_rebuilds_today", Action: "delete-missing", WantRows: hubImportIDs(a, b, c, d), WantRecords: hubImportIDs(a, b, c, d), WantDates: map[string]int{today: 4}, WantActive: today},
			{Name: "append_duplicate_after_rebuild", Action: "import", Strategy: "append", Date: date3, Rows: hubImportText(a), WantRows: hubImportIDs(a, b, c, d), WantRecords: hubImportIDs(a, b, c, d, a), WantDates: map[string]int{today: 4, date3: 5}, WantActive: date3},
			{Name: "delete_disabled_compacts_and_removes_snapshot_duplicates", Action: "delete", DeleteURL: b.TargetURL, WantRows: hubImportIDs(a, c, d), WantRecords: hubImportIDs(a, c, d), WantDates: map[string]int{today: 3}, WantActive: today},
			{Name: "clear_removes_all_dates_and_sequence", Action: "clear", WantRows: []HubExternalRecord{}, WantRecords: []HubExternalRecord{}, WantDates: map[string]int{}},
			{Name: "append_after_clear_starts_at_one", Action: "import", Strategy: "append", Date: date1, Rows: hubImportText(a), WantRows: hubImportIDs(a), WantRecords: hubImportIDs(a), WantDates: map[string]int{date1: 1}, WantActive: date1},
			{Name: "replace_resets_history_and_ids", Action: "import", Strategy: "replace", Date: date2, Rows: hubImportText(c), WantRows: hubImportIDs(c), WantRecords: hubImportIDs(c), WantDates: map[string]int{date2: 1}, WantActive: date2},
			{Name: "delete_last_leaves_no_today_zero_boundary", Action: "delete", DeleteURL: c.TargetURL, WantRows: []HubExternalRecord{}, WantRecords: []HubExternalRecord{}, WantDates: map[string]int{}},
		}},
		{Name: "delete_existing_rebuilds_history", Operations: []hubImportOperation{
			{Name: "replace_historical", Action: "import", Strategy: "replace", Date: date1, Rows: hubImportText(a, b, c), WantRows: hubImportIDs(a, b, c), WantRecords: hubImportIDs(a, b, c), WantDates: map[string]int{date1: 3}, WantActive: date1},
			{Name: "append_later", Action: "import", Strategy: "append", Date: date2, Rows: hubImportText(d, e), WantRows: hubImportIDs(a, b, c, d, e), WantRecords: hubImportIDs(a, b, c, d, e), WantDates: map[string]int{date1: 3, date2: 5}, WantActive: date2},
			{Name: "delete_enabled_rebuilds_today_including_disabled", Action: "delete", DeleteURL: c.TargetURL, WantRows: hubImportIDs(a, b, d, e), WantRecords: hubImportIDs(a, b, d, e), WantDates: map[string]int{today: 4}, WantActive: today},
			{Name: "append_after_delete_uses_rebuilt_sequence", Action: "import", Strategy: "append", Date: date3, Rows: hubImportText(c), WantRows: hubImportIDs(a, b, d, e, c), WantRecords: hubImportIDs(a, b, d, e, c), WantDates: map[string]int{today: 4, date3: 5}, WantActive: date3},
		}},
	}
}

func newHubImportHarness(t *testing.T) *adminHarness {
	h := newAdminHarness(t, nil)
	// Deliberately consume unrelated SQL IDs. They must never leak into either
	// the ordinary logical-ID namespace or the independent snapshot namespace.
	for i := 0; i < 9; i++ {
		row := SiteLink{SiteKey: "preview", Kind: "external", Language: "id", Domain: "preview.example", AnchorText: fmt.Sprint(i), TargetURL: fmt.Sprintf("https://preview.example/%d", i), Enabled: true}
		if err := h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := h.rt.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.login()
	h.call("POST", "/admin/api/hub/config", map[string]any{"enabled": true, "external_min": 2, "external_max": 3, "bottom_link_count": 2}, 200)
	return h
}

func hubImportApply(t *testing.T, h *adminHarness, op hubImportOperation) {
	t.Helper()
	h.t = t
	switch op.Action {
	case "import":
		h.call("POST", "/admin/api/sites/links/import", map[string]any{"site_key": "default", "type": "external", "strategy": op.Strategy, "effective_date": op.Date, "links": op.Rows}, 200)
	case "clear":
		h.call("DELETE", "/admin/api/external-links", map[string]any{"site_key": "default", "all": true}, 200)
	case "delete", "delete-missing":
		id := uint(999999)
		if op.Action == "delete" {
			id = 0
			for _, l := range h.rt.Snapshot().Links {
				if l.SiteKey == "default" && l.Kind == "external" && l.TargetURL == op.DeleteURL {
					id = l.ID
					break
				}
			}
			if id == 0 {
				t.Fatalf("no Go SQL handle for PHP delete target %q", op.DeleteURL)
			}
		}
		h.call("DELETE", "/admin/api/external-links", map[string]any{"site_key": "default", "ids": []uint{id}}, 200)
	default:
		t.Fatalf("unknown operation %q", op.Action)
	}
}

func hubImportRows(snapshot *Snapshot) []HubExternalRecord {
	rows := []HubExternalRecord{}
	for _, r := range LegacySnapshotFromRuntime(snapshot).ExternalLinks {
		rows = append(rows, HubExternalRecord{ID: int(r.ID), Domain: r.Domain, AnchorText: r.AnchorText, TargetURL: r.TargetURL, Weight: r.Weight, Enabled: r.Enabled})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}
func hubImportRecords(rows []HubExternalRecord) map[int]HubExternalRecord {
	out := map[int]HubExternalRecord{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func hubImportExpectedSources(op hubImportOperation, previous map[int]hubImportSourceRecord, today string) map[int]hubImportSourceRecord {
	want := map[int]hubImportSourceRecord{}
	for _, row := range op.WantRecords {
		if old, ok := previous[row.ID]; ok && op.Action == "import" && op.Strategy == "append" {
			want[row.ID] = old
			continue
		}
		date := today
		if op.Action == "import" {
			date = op.Date
		}
		want[row.ID] = hubImportSourceRecord{HubExternalRecord: row, SnapshotID: row.ID, FirstSeenDate: date, Language: "id", Category: "tools", Remark: "source row", SourceFile: "admin_stats_textarea"}
	}
	return want
}

func TestHubImportAPISnapshotMutationContract(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	for _, sequence := range hubImportSequences(today) {
		t.Run(sequence.Name, func(t *testing.T) {
			h := newHubImportHarness(t)
			var expectedSources map[int]hubImportSourceRecord
			for _, op := range sequence.Operations {
				expectedSources = hubImportExpectedSources(op, expectedSources, today)
				t.Run(op.Name, func(t *testing.T) {
					old := h.rt.Snapshot()
					before, _ := json.Marshal(old.Hub)
					hubImportApply(t, h, op)
					after, _ := json.Marshal(old.Hub)
					if !bytes.Equal(before, after) {
						t.Error("mutation changed a published old snapshot")
					}
					s := h.rt.Snapshot()
					if got := hubImportRows(s); !reflect.DeepEqual(got, op.WantRows) {
						t.Errorf("ordinary logical rows differ\nGo: %v\nPHP:%v", got, op.WantRows)
					}
					if !reflect.DeepEqual(s.Hub.SnapshotMaxIDs, op.WantDates) || s.Hub.ActiveDate != op.WantActive {
						t.Errorf("snapshot dates: Go=%v active=%q PHP=%v active=%q", s.Hub.SnapshotMaxIDs, s.Hub.ActiveDate, op.WantDates, op.WantActive)
					}
					if want := hubImportRecords(op.WantRecords); !reflect.DeepEqual(s.Hub.ExternalRecords, want) {
						t.Errorf("independent snapshot payloads/IDs\nGo: %v\nPHP:%v", s.Hub.ExternalRecords, want)
					}
					if got := hubImportSources(t, h); !reflect.DeepEqual(got, expectedSources) {
						t.Errorf("original source payloads/first-seen dates\nGo: %v\nPHP:%v", got, expectedSources)
					}
					// Restart against this private DB and verify publication reads exactly
					// what the mutation persisted, not a process-local correction.
					restarted, err := NewRuntime(context.Background(), h.db, RuntimeOptions{LanguageRoot: h.lan, AssetRoot: h.assets})
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(s.Hub, restarted.Snapshot().Hub) {
						t.Error("restart changed configured hub snapshot")
					}
					if got := hubImportRows(restarted.Snapshot()); !reflect.DeepEqual(got, op.WantRows) {
						t.Errorf("logical rows not persisted after restart: %v", got)
					}
				})
			}
		})
	}
}

func hubImportPublic(t *testing.T, rt *Runtime, uri, ua string) string {
	t.Helper()
	router := NewRouterWithRuntime(rt, RouterOptions{})
	r := httptest.NewRequest("GET", "http://hub-import.example"+uri, nil)
	r.RequestURI = uri
	r.Header.Set("User-Agent", ua)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("hub public status %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}
