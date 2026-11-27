package site

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var legacyCategories = []string{"productivity", "tools", "education", "health", "music"}
var legacyCategoryText = map[string]map[string]string{
	"productivity": {"en": "Productivity", "id": "Produktivitas", "th": "เพิ่มประสิทธิภาพ", "vn": "Nang suat"},
	"tools":        {"en": "Tools", "id": "Alat", "th": "เครื่องมือ", "vn": "Cong cu"},
	"education":    {"en": "Education", "id": "Pendidikan", "th": "การศึกษา", "vn": "Giao duc"},
	"health":       {"en": "Health", "id": "Kesehatan", "th": "สุขภาพ", "vn": "Suc khoe"},
	"music":        {"en": "Music", "id": "Musik", "th": "เพลง", "vn": "Am nhac"},
	"apps":         {"en": "Apps", "id": "Aplikasi", "th": "แอป", "vn": "Ung dung"},
}

func legacyCategory(category, lang string) string {
	category = legacyLower(legacyTrim(category))
	if row, ok := legacyCategoryText[category]; ok {
		if text, ok := row[lang]; ok {
			return text
		}
		return row["en"]
	}
	if len(category) > 0 {
		return strings.ToUpper(category[:1]) + category[1:]
	}
	return category
}
func legacyUnique(items []string, fold bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		key := item
		if fold {
			key = legacyLower(legacyTrim(key))
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func legacyPickWords(words []string, hash string, count int) []string {
	return selectSequential(legacyUnique(words, true), hash, count, 0, "offset")
}
func legacyTitle(brand, hash, lang string, s LegacySnapshot) string {
	categories := s.Categories
	if categories == nil {
		categories = legacyCategories
	}
	categories = legacyUnique(categories, true)
	category := "Apps"
	if len(categories) > 0 {
		v, _ := strconv.ParseUint(hash[8:12], 16, 16)
		category = legacyCategory(categories[int(v)%len(categories)], lang)
	}
	if suffixes := s.TitleSuffixes[lang]; len(suffixes) > 0 {
		suffix := suffixes[int(hashUint32(sha1Hex(legacyLower(lang)+"|title_suffix|"+hash)))%len(suffixes)]
		if strings.Contains(suffix, "{keyword}") || strings.Contains(suffix, "{category}") {
			return strings.NewReplacer("{keyword}", brand, "{category}", category).Replace(suffix)
		}
		if strings.Count(suffix, "%s") > 0 {
			suffix = strings.Replace(suffix, "%s", brand, 1)
			suffix = strings.Replace(suffix, "%s", category, 1)
			return suffix
		}
		return legacyTrim(brand + " " + suffix)
	}
	switch lang {
	case "id":
		return brand + " Unduh APK - Aplikasi Android " + category
	case "th":
		return brand + " ดาวน์โหลด APK - แอป Android หมวด" + category
	case "vn":
		return brand + " Tai APK - Ung dung Android " + category
	default:
		return brand + " APK Download - " + category + " Android App"
	}
}

func legacyParagraphs(sentences []string, title, brand string, words []string) []string {
	if len(sentences) >= 4 {
		return append([]string(nil), sentences[:4]...)
	}
	secondary := "apk"
	if len(words) > 1 {
		secondary = words[1]
	}
	return []string{
		fmt.Sprintf("%s focuses on %s, %s, and practical Android usage for visitors who need a direct APK page.", title, brand, secondary),
		"The page is generated from a stable URL hash, so repeated visits keep the same title, modules, FAQ, and related links.",
		"Core content is rendered from local snapshots and word lan. Public traffic does not need a MySQL query on every request.",
		"Keyword coverage is expanded with " + strings.Join(words[:min(12, len(words))], ", ") + " so list pages, subdomain mirrors, and external download pages can keep different wording without duplicate picks.",
	}
}

func legacyDescription(p LegacyPage) string {
	brand := strings.TrimSpace(p.Brand)
	if brand == "" {
		brand = "APK"
	}
	version := strings.TrimSpace(p.Version)
	if version == "" {
		version = "latest"
	}
	size := strings.TrimSpace(p.FileSize)
	if size == "" {
		size = "APK"
	}

	var text string
	switch p.Language {
	case "id":
		text = fmt.Sprintf("Unduh %s %s APK untuk Android. Ukuran file %s, lengkap dengan versi terbaru dan tautan unduhan resmi.", brand, version, size)
	case "th":
		text = fmt.Sprintf("ดาวน์โหลด %s %s APK สำหรับ Android ขนาดไฟล์ %s พร้อมเวอร์ชันล่าสุดและลิงก์ดาวน์โหลดทางการ", brand, version, size)
	case "vn":
		text = fmt.Sprintf("Tải %s %s APK cho Android. Dung lượng tệp %s, kèm phiên bản mới nhất và liên kết tải chính thức.", brand, version, size)
	default:
		text = fmt.Sprintf("Download %s %s APK for Android. File size %s, with the latest version details and official download link.", brand, version, size)
	}
	runes := []rune(text)
	if len(runes) > 160 {
		text = string(runes[:157]) + "..."
	}

	return text
}

func legacyShuffle[T any](items []T, key func(T) string) []T {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

func legacySlug(s string) string {
	if slug := slugifyPHP(s); slug != "" {
		return slug
	}
	return "apk"
}
