package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	gormclause "gorm.io/gorm/clause"
)

type seedApp struct {
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	PackageName string      `json:"package_name"`
	Developer   string      `json:"developer"`
	Version     string      `json:"version"`
	Category    string      `json:"category"`
	Language    string      `json:"language"`
	Description string      `json:"description"`
	Rating      string      `json:"rating"`
	Downloads   string      `json:"downloads"`
	IconPath    string      `json:"icon_path"`
	SourceURL   string      `json:"source_url"`
	SourceName  string      `json:"source_name"`
	CapturedAt  time.Time   `json:"captured_at"`
	Screenshots []seedImage `json:"screenshots"`
}

type seedImage struct {
	LocalPath string `json:"local_path"`
	SourceURL string `json:"source_url"`
}

type seedLink struct {
	AppSlug    string `json:"app_slug"`
	Kind       string `json:"kind"`
	Language   string `json:"language"`
	Domain     string `json:"domain"`
	AnchorText string `json:"anchor_text"`
	TargetURL  string `json:"target_url"`
	Category   string `json:"category"`
	Weight     int    `json:"weight"`
	Remark     string `json:"remark"`
}

func Seed(db *gorm.DB, seedPath string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// Existing releases created this unique default row in the same transaction
		// as the samples. Its presence means the install is no longer pristine.
		var count int64
		if err := tx.Model(&SiteConfig{}).Where("site_key = ?", "default").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		data, err := os.ReadFile(seedPath)
		if err != nil {
			return fmt.Errorf("read seed file: %w", err)
		}
		var records []seedApp
		if err := json.Unmarshal(data, &records); err != nil {
			return fmt.Errorf("decode seed file: %w", err)
		}
		seedAbsolute, err := filepath.Abs(seedPath)
		if err != nil {
			return err
		}
		assetRoot := os.Getenv("ASSET_DIR")
		if assetRoot == "" {
			assetRoot = filepath.Clean(filepath.Join(filepath.Dir(seedAbsolute), "../..", "assets"))
		}

		for _, record := range records {
			if err := validateSeed(record, assetRoot); err != nil {
				return err
			}
			app := App{
				Slug: record.Slug, Name: record.Name, PackageName: record.PackageName,
				Developer: record.Developer, Version: record.Version, Category: record.Category,
				Language: record.Language, Description: record.Description, Rating: record.Rating,
				Downloads: record.Downloads, IconPath: record.IconPath, SourceURL: record.SourceURL,
				SourceName: record.SourceName, CapturedAt: record.CapturedAt, TemporaryData: true, Active: true,
			}
			var existing App
			err := tx.Where("slug = ?", record.Slug).First(&existing).Error
			if err == nil && !existing.TemporaryData {
				continue
			}
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			if existing.ID != 0 {
				app.ID = existing.ID
			}
			if err := tx.Clauses(gormclause.OnConflict{UpdateAll: true}).Create(&app).Error; err != nil {
				return err
			}
			if err := tx.Where("app_id = ?", app.ID).Delete(&AppScreenshot{}).Error; err != nil {
				return err
			}
			for position, image := range record.Screenshots {
				if err := tx.Create(&AppScreenshot{AppID: app.ID, Position: position, LocalPath: image.LocalPath, SourceURL: image.SourceURL}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Clauses(gormclause.OnConflict{DoNothing: true}).Create(&SiteConfig{
			SiteKey: "default", DisplayName: "APK Hub", DefaultLanguage: "id", RoutePrefix: "",
			DownloadBaseURL: "https://download.invalid/apk/", ImageBasePath: "/assets", ExternalLinkLimit: 12, MaxRelatedLinks: 6,
		}).Error; err != nil {
			return err
		}
		// Prototype link fixtures belong to the explicit noindex sample preview.
		// Public default pages use the PHP-generated collections until an admin
		// adds real managed links; fixtures must not alter the hub link pool.
		if err := tx.Clauses(gormclause.OnConflict{DoNothing: true}).Create(&SiteConfig{
			SiteKey: "preview", DisplayName: "APK Hub Samples", DefaultLanguage: "id", RoutePrefix: "",
			DownloadBaseURL: "https://download.invalid/apk/", ImageBasePath: "/assets", ExternalLinkLimit: 12, MaxRelatedLinks: 6,
		}).Error; err != nil {
			return err
		}
		return seedLinks(tx, filepath.Join(filepath.Dir(seedAbsolute), "links.json"))
	})
}

func seedLinks(tx *gorm.DB, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read link seed file: %w", err)
	}
	var records []seedLink
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("decode link seed file: %w", err)
	}
	for _, record := range records {
		if record.Kind != "internal" && record.Kind != "external" {
			return fmt.Errorf("invalid link kind %q", record.Kind)
		}
		if record.AnchorText == "" || record.TargetURL == "" || record.Language == "" {
			return fmt.Errorf("invalid link seed: anchor, target and language are required")
		}
		if record.Weight <= 0 {
			record.Weight = 1
		}
		var link SiteLink
		query := tx.Where("site_key = ? AND app_slug = ? AND kind = ? AND anchor_text = ? AND target_url = ?",
			"preview", record.AppSlug, record.Kind, record.AnchorText, record.TargetURL)
		if err := query.First(&link).Error; err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		link.SiteKey = "preview"
		link.AppSlug = record.AppSlug
		link.Kind = record.Kind
		link.Language = record.Language
		link.Domain = record.Domain
		link.AnchorText = record.AnchorText
		link.TargetURL = record.TargetURL
		link.Category = record.Category
		link.Weight = record.Weight
		link.Enabled = true
		link.Remark = record.Remark
		if err := tx.Save(&link).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateSeed(record seedApp, assetRoot string) error {
	if record.Slug == "" || record.Name == "" || record.PackageName == "" || !strings.HasPrefix(record.SourceURL, "https://") {
		return fmt.Errorf("invalid seed app %q: slug, name, package and HTTPS source URL are required", record.Slug)
	}
	for _, image := range append([]seedImage{{LocalPath: record.IconPath}}, record.Screenshots...) {
		clean := filepath.Clean(image.LocalPath)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("invalid local image path %q for %s", image.LocalPath, record.Slug)
		}
		if _, err := os.Stat(filepath.Join(assetRoot, clean)); err != nil {
			return fmt.Errorf("missing local image %q for %s: %w", image.LocalPath, record.Slug, err)
		}
	}
	return nil
}
