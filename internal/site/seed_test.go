package site

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSampleSeedUsesLocalImagesAndLocalCatalogSources(t *testing.T) {
	data, err := os.ReadFile("../../data/seed/apps.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []seedApp
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) < 3 {
		t.Fatalf("expected at least 3 seeded apps, got %d", len(records))
	}
	contact := regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}|support email|business email|customer center|developer contact`)
	slugs, packages := map[string]bool{}, map[string]bool{}
	for _, record := range records {
		if !strings.HasPrefix(record.SourceURL, "https://") {
			t.Errorf("unexpected source for %s: %s", record.Slug, record.SourceURL)
		}
		if len(record.Screenshots) < 1 {
			t.Errorf("expected at least one screenshot for %s", record.Slug)
		}
		if err := validateSeed(record, "../../assets"); err != nil {
			t.Errorf("invalid seed %s: %v", record.Slug, err)
		}
		if contact.MatchString(record.Description) {
			t.Errorf("seed %s contains contact information", record.Slug)
		}
		if slugs[record.Slug] || packages[record.PackageName] {
			t.Errorf("duplicate seed slug/package: %s / %s", record.Slug, record.PackageName)
		}
		slugs[record.Slug], packages[record.PackageName] = true, true
	}
}
