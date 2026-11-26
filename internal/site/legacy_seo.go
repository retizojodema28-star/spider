package site

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// IndexLegacyLocalImageFiles runs at startup/snapshot refresh, never per request.
// Keys use the server's /assets mount. No network requests or invented filenames.
func IndexLegacyLocalImageFiles(assetRoot string) (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir(assetRoot, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(file)) {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		default:
			return nil
		}
		rel, err := filepath.Rel(assetRoot, file)
		if err != nil {
			return err
		}
		files["/assets/"+filepath.ToSlash(rel)] = true
		return nil
	})
	return files, err
}

func legacyLocalImages(p LegacyPage, cfg SiteConfig, s LegacySnapshot) LegacyImages {
	images := LegacyImages{Screenshots: []string{}, SEO: []string{}}
	base := cfg.ImageBasePath
	for _, host := range []string{p.Host, p.EntryHost} {
		found := false
		for _, binding := range s.ImageBindings {
			if normalizePHPHost(binding.Domain) == host && binding.ImageBasePath != "" {
				base = binding.ImageBasePath
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if base == "" {
		base = "/assets"
	}
	if !legacyLocalPath(base) {
		base = "/assets"
	}
	// PHP's directory contract is base/{brand}/1..5. Only existing files enter
	// the model. Bindings select a directory; they don't rebase sample fixtures.
	imageSlug := slugifyPHP(p.Brand)
	if imageSlug == "" {
		imageSlug = "default"
	}
	for i := 1; i <= 5; i++ {
		for _, ext := range []string{"jpg", "webp", "png", "jpeg", "avif"} {
			file := "/" + strings.TrimPrefix(path.Join(base, imageSlug, legacyInt(i)+"."+ext), "/")
			if s.LocalImageFiles[file] {
				images.Screenshots = append(images.Screenshots, file)
				break
			}
		}
	}
	if len(images.Screenshots) > 0 {
		images.Primary = images.Screenshots[0]
		for _, file := range images.Screenshots {
			images.SEO = append(images.SEO, p.SEOBaseURL+file)
		}
		return images
	}
	local := func(file string) string {
		if !legacyLocalPath(file) {
			return ""
		}
		file = strings.TrimPrefix(strings.TrimPrefix(file, "/"), "assets/")
		file = "/assets/" + strings.TrimPrefix(path.Clean(file), "/")
		if s.LocalImageFiles != nil && !s.LocalImageFiles[file] {
			return ""
		}
		return file
	}
	// A database row may outlive its downloaded asset. Filter candidates by the
	// indexed files before selecting one so stale paths can never reach HTML.
	type localApp struct {
		app   App
		icon  string
		shots []string
	}
	apps := []localApp{}
	for _, app := range s.Apps {
		if !app.Active || app.IconPath == "" {
			continue
		}
		icon := local(app.IconPath)
		if icon == "" {
			continue
		}
		candidate := localApp{app: app, icon: icon}
		for _, shot := range app.Screenshots {
			if file := local(shot.LocalPath); file != "" {
				candidate.shots = append(candidate.shots, file)
			}
		}
		apps = append(apps, candidate)
	}
	if len(apps) > 0 {
		sort.SliceStable(apps, func(i, j int) bool { return apps[i].app.Slug < apps[j].app.Slug })
		chosen := apps[int(hashUint32(p.Hash))%len(apps)]
		for _, app := range apps {
			if strings.TrimPrefix(p.ContentPath, "/apps/") == app.app.Slug {
				chosen = app
				break
			}
		}
		images.Primary = chosen.icon
		images.Screenshots = append(images.Screenshots, chosen.shots...)
	}
	// The current installation can contain only local sample assets while the
	// database still has older catalogue rows. Use a coherent indexed sample set
	// as the final local fallback instead of returning 404 image URLs.
	if images.Primary == "" {
		images.Primary, images.Screenshots = legacyIndexedSampleImages(s.LocalImageFiles, p.Hash)
	}
	if images.Primary != "" && len(images.Screenshots) == 0 {
		images.Screenshots = append(images.Screenshots, images.Primary)
	}
	images.Screenshots = images.Screenshots[:min(5, len(images.Screenshots))]
	for _, file := range legacyUnique(append([]string{images.Primary}, images.Screenshots...), false) {
		images.SEO = append(images.SEO, p.SEOBaseURL+file)
	}
	images.SEO = images.SEO[:min(5, len(images.SEO))]
	return images
}

func legacyIndexedSampleImages(files map[string]bool, hash string) (string, []string) {
	if files == nil {
		return "", nil
	}
	type sample struct {
		icon  string
		shots []string
	}
	samples := map[string]*sample{}
	for file := range files {
		if !strings.HasPrefix(file, "/assets/") {
			continue
		}
		name := path.Base(file)
		ext := path.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		switch {
		case strings.HasSuffix(stem, "-icon"):
			key := strings.TrimSuffix(stem, "-icon")
			if key == "" {
				continue
			}
			if samples[key] == nil {
				samples[key] = &sample{}
			}
			samples[key].icon = file
		case strings.Contains(stem, "-shot-"):
			key := stem[:strings.LastIndex(stem, "-shot-")]
			if key == "" {
				continue
			}
			if samples[key] == nil {
				samples[key] = &sample{}
			}
			samples[key].shots = append(samples[key].shots, file)
		}
	}
	keys := make([]string, 0, len(samples))
	for key, item := range samples {
		if item.icon != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", nil
	}
	key := keys[int(hashUint32(hash+"|local-sample"))%len(keys)]
	item := samples[key]
	sort.Strings(item.shots)
	return item.icon, append([]string(nil), item.shots...)
}

func legacyLocalPath(file string) bool {
	if file == "" || strings.HasPrefix(file, "//") || strings.ContainsAny(file, "\\?#\x00") || legacyScheme.MatchString(file) {
		return false
	}
	for _, segment := range strings.Split(file, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// The structured application metadata is generated from the local page model.
// The service does not fetch, serve, or proxy application binaries.
func legacyBuildSEO(p *LegacyPage) {
	labels := legacyLabelsFor(p.Language).SEO
	p.DisplayTitle = p.Brand + " ⚡ " + labels.TitleSuffix
	keywords := []string{}
	for _, keyword := range labels.Keywords {
		keywords = append(keywords, strings.ReplaceAll(keyword, "{brand}", p.Brand))
	}
	p.SEOKeywords = strings.Join(keywords, ", ")
	p.OGLocale = labels.OGLocale
	p.Robots = "index, follow, max-image-preview:large"
	if p.Preview {
		p.Robots = "noindex, nofollow"
	}
	hero := p.Images.Primary
	if hero != "" {
		hero = p.SEOBaseURL + hero
	}
	shots, gallery := []any{}, []any{}
	for i, file := range p.Images.SEO {
		shots = append(shots, map[string]any{"@type": "ImageObject", "url": file, "name": p.Brand + " " + legacyInt(i+1), "caption": p.Brand})
		gallery = append(gallery, map[string]any{"@type": "ImageObject", "contentUrl": file, "name": p.Brand + " - " + legacyInt(i+1), "description": p.Brand, "representativeOfPage": i == 0})
	}
	faq := []any{}
	for _, item := range p.FAQ {
		faq = append(faq, map[string]any{"@type": "Question", "name": item.Question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": item.Answer}})
	}
	nodes := []any{
		map[string]any{"@context": "https://schema.org", "@type": "WebPage", "url": p.Canonical, "name": p.DisplayTitle, "description": p.Description, "image": p.Images.SEO, "thumbnailUrl": p.Images.SEO, "primaryImageOfPage": map[string]any{"@type": "ImageObject", "url": hero, "caption": p.Brand}},
		map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": p.DisplayTitle, "url": p.SEOBaseURL + "/", "publisher": map[string]any{"@type": "Organization", "name": p.DisplayTitle, "logo": map[string]any{"@type": "ImageObject", "url": hero}}},
		map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": p.Brand, "description": strings.Join(p.Paragraphs[:min(3, len(p.Paragraphs))], " "), "operatingSystem": labels.OperatingSystem, "applicationCategory": labels.ApplicationCategory, "softwareVersion": "3.5.2", "fileSize": "28 MB", "screenshot": shots, "offers": map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "IDR"}, "aggregateRating": map[string]any{"@type": "AggregateRating", "ratingValue": p.RatingValue, "ratingCount": p.RatingCount, "bestRating": "5"}},
		map[string]any{"@context": "https://schema.org", "@type": "ImageGallery", "name": p.Brand + " " + labels.GallerySuffix, "url": p.SEOBaseURL + "/", "image": gallery},
		map[string]any{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": faq},
	}
	for _, node := range nodes {
		// Marshal's HTML escaping prevents a bank sentence from closing <script>.
		// JSON is semantically identical to PHP; content_hash uses legacyJSON.
		encoded, _ := json.Marshal(node)
		p.JSONLD = append(p.JSONLD, template.JS(encoded))
	}
}

func legacyInt(n int) string { b, _ := json.Marshal(n); return string(b) }
