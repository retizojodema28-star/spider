package site

import (
	"bytes"
	"encoding/json"
	"html/template"
	"path/filepath"
	"strconv"
	"testing"
)

func legacyTestBank(t *testing.T) *LanguageBank {
	t.Helper()
	bank, err := LoadLanguageBank(filepath.Join("..", "..", "data", "lan"), "id")
	if err != nil {
		t.Fatal(err)
	}
	return bank
}

func legacyTestApps() []App {
	return []App{
		{Slug: "sample-app-one", Name: "Sample App One", Version: "1.0.0", Language: "id", Active: true, IconPath: "samples/sample-app-one-icon.jpg", Screenshots: []AppScreenshot{{LocalPath: "samples/sample-app-one-shot-1.jpg"}, {LocalPath: "samples/sample-app-one-shot-2.jpg"}}},
		{Slug: "sample-app-two", Name: "Sample App Two", Version: "2.3.1", Language: "id", Active: true, IconPath: "samples/sample-app-two-icon.png", Screenshots: []AppScreenshot{{LocalPath: "samples/sample-app-two-shot-1.jpg"}, {LocalPath: "samples/sample-app-two-shot-2.jpg"}}},
		{Slug: "sample-app-three", Name: "Sample App Three", Version: "3.2.0", Language: "id", Active: true, IconPath: "samples/sample-app-three-icon.png", Screenshots: []AppScreenshot{{LocalPath: "samples/sample-app-three-shot-1.jpg"}, {LocalPath: "samples/sample-app-three-shot-2.jpg"}}},
	}
}

func legacyRenderForTest(t *testing.T, page LegacyPage) string {
	t.Helper()
	tpl := template.Must(template.New("pages").Funcs(template.FuncMap{
		"requestImage": func(path string) string { return "/assets/" + path },
		"json":         func(value any) template.JS { data, _ := json.Marshal(value); return template.JS(data) },
	}).ParseFS(templateFiles, "templates/*.html"))
	var body bytes.Buffer
	if err := tpl.ExecuteTemplate(&body, "legacy.html", page); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

type hubImportSourceRecord struct {
	HubExternalRecord
	SnapshotID    int
	FirstSeenDate string
	Language      string
	Category      string
	Remark        string
	SourceFile    string
}

func hubImportSources(t *testing.T, h *adminHarness) map[int]hubImportSourceRecord {
	t.Helper()
	var records []RuntimeExternalRecord
	if err := h.db.Order("id asc").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	var metadata []RuntimeLinkMetadata
	if err := h.db.Find(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, row := range metadata {
		sources[strconv.FormatUint(uint64(row.LinkID), 10)] = row.SourceFile
	}
	out := map[int]hubImportSourceRecord{}
	for _, record := range records {
		var payload struct {
			Domain     string `json:"domain"`
			Language   string `json:"lang"`
			AnchorText string `json:"anchor_text"`
			TargetURL  string `json:"target_url"`
			Category   string `json:"category"`
			Weight     int    `json:"weight"`
			Enabled    bool   `json:"enabled"`
			Remark     string `json:"remark"`
			SourceFile string `json:"source_file"`
		}
		if err := json.Unmarshal([]byte(record.Payload), &payload); err != nil {
			t.Fatalf("decode source payload %d: %v", record.ID, err)
		}
		source := payload.SourceFile
		if source == "" {
			source = sources[strconv.FormatUint(uint64(record.LinkID), 10)]
		}
		id := int(record.ID)
		out[id] = hubImportSourceRecord{
			HubExternalRecord: HubExternalRecord{ID: id, Domain: payload.Domain, AnchorText: payload.AnchorText, TargetURL: payload.TargetURL, Weight: payload.Weight, Enabled: payload.Enabled},
			SnapshotID:        id,
			FirstSeenDate:     record.FirstSeenDate,
			Language:          payload.Language,
			Category:          payload.Category,
			Remark:            payload.Remark,
			SourceFile:        source,
		}
	}
	return out
}
