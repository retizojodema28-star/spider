package site

// hubLocalImages shares detail-page image resolution: site image base, domain
// then entry-domain binding, existing indexed brand files, and actual sample
// fixtures. It retains the shared resolver's compatibility behavior for callers
// without an index: only supplied sample metadata can provide fallback paths.
func hubLocalImages(request LegacyPage, config SiteConfig, snapshot LegacySnapshot, brand, hash string) HubImages {
	request.Brand = brand
	request.Hash = hash
	request.SEOBaseURL = "https://" + request.Host
	local := legacyLocalImages(request, config, snapshot)
	images := HubImages{Primary: local.Primary, Screenshots: append([]string{}, local.Screenshots...), Alt: brand + " image"}
	if images.Primary == "" && len(images.Screenshots) > 0 {
		images.Primary = images.Screenshots[0]
	}
	return images
}
