package site

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var phpKeywordSuffixes = []string{
	"APK", "APK download", "download APK", "latest version", "old version", "Android app",
	"Android game", "official app", "safe download", "fast download", "APK free",
	"APK update", "screenshots", "APK install", "XAPK install", "app review",
}

func phpPublicBaseURL(scheme, requestHost string) string {
	host := normalizePHPHost(requestHost)
	if host == "" {
		host = "localhost"
	}
	return strings.ToLower(scheme) + "://" + host
}

func phpRouteHash(scheme, requestHost, language, requestURI string) string {
	host := normalizePHPHost(requestHost)
	input := strings.ToLower(scheme) + "|" + host + "|" + strings.ToLower(language) + "|" + host + "|" + requestURI
	digest := sha1.Sum([]byte(input))
	return hex.EncodeToString(digest[:])
}

func phpKeywordChips(words []string, hash, brand string) []string {
	if brand == "" {
		brand = "APK"
	}
	phrases := make([]string, 0, len(phpKeywordSuffixes)+8)
	for _, suffix := range phpKeywordSuffixes {
		phrases = append(phrases, brand+" "+suffix)
	}
	if len(words) > 1 {
		for _, word := range words[1:min(len(words), 9)] {
			phrases = append(phrases, brand+" vs "+word)
		}
	}
	unique := make([]string, 0, len(phrases))
	seen := make(map[string]struct{}, len(phrases))
	for _, phrase := range phrases {
		if _, ok := seen[phrase]; ok {
			continue
		}
		seen[phrase] = struct{}{}
		unique = append(unique, phrase)
	}
	digest := sha1.Sum([]byte(hash))
	limit := 10 + int(uint32(digest[0])<<24|uint32(digest[1])<<16|uint32(digest[2])<<8|uint32(digest[3]))%11
	return unique[:min(len(unique), limit)]
}

func slugifyPHP(value string) string {
	return strings.Trim(nonAlphaNum.ReplaceAllString(legacyLower(value), "-"), "-")
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// phpCanonicalURL matches SeoRenderer: HTTPS, normalized host, no request port,
// and the normalized public path. Navigation targets follow their PHP-specific forms.
func phpCanonicalURL(requestHost, requestPath string) string {
	host := normalizePHPHost(requestHost)
	if host == "" {
		host = "localhost"
	}
	return "https://" + host + normalizePHPPath(requestPath)
}

func normalizePHPHost(host string) string {
	host = legacyLower(legacyTrim(host))
	return legacyPort.ReplaceAllString(host, "")
}

func normalizePHPPath(requestPath string) string {
	path := legacyLower(legacyUnescape(legacyURLPath(requestPath), false))
	segments := strings.Split(path, "/")
	clean := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "", ".":
			continue
		case "..":
			if len(clean) > 0 {
				clean = clean[:len(clean)-1]
			}
		default:
			clean = append(clean, segment)
		}
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}

// phpPageType mirrors DynamicPageService.panDuanYeMianLeiXing for the
// host/path combinations that select the legacy renderer family.
func phpPageType(host, requestPath string) string {
	return legacyPageTypeNormalized(normalizePHPHost(host), normalizePHPPath(requestPath))
}

// DynamicPageService normalizes once. Re-normalizing a decoded %3F or %252F
// here would treat path bytes as a query/second escape and change page_type.
func legacyPageTypeNormalized(host, path string) string {
	for _, prefix := range []string{"/go", "/out", "/external", "/link"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return "external"
		}
	}
	if host != "localhost" && net.ParseIP(host) == nil && strings.Count(host, ".") >= 2 {
		return "subdomain"
	}
	if path == "/" || path == "/apps" || path == "/category" || path == "/categories" || path == "/list" || path == "/tag" || path == "/tags" {
		return "list"
	}
	return "detail"
}

// Default site categories, in core/config/sites.php order. TitleComposer
// localizes the category selected by QueDingXingPicker at offset 2.
func phpDefaultIndonesianTitle(brand, routeHash string) string {
	categories := [...]string{"Produktivitas", "Alat", "Pendidikan", "Kesehatan", "Musik"}
	slot, _ := strconv.ParseUint(routeHash[8:12], 16, 16)
	return brand + " Unduh APK - Aplikasi Android " + categories[int(slot)%len(categories)]
}

// EntryParams and HubSubdomainGenerator remove exactly one HTTP(S) prefix.
// Stripping both sequentially would accept https://http://example.com.
func normalizePHPDomain(domain string) string {
	domain = legacyLower(legacyTrim(domain))
	if strings.HasPrefix(domain, "https://") {
		domain = strings.TrimPrefix(domain, "https://")
	} else {
		domain = strings.TrimPrefix(domain, "http://")
	}
	domain = strings.Trim(domain, "/")
	if !legacyDomain.MatchString(domain) {
		return ""
	}
	return domain
}

func legacyRequestScheme(r *http.Request) string {
	forwarded := legacyLower(legacyTrim(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]))
	if forwarded == "http" || forwarded == "https" {
		return forwarded
	}
	// json_decode(..., true) looks up the exact key. Go struct decoding is
	// case-insensitive and can also reject an unrelated mixed-case key's type.
	var visitor map[string]json.RawMessage
	if json.Unmarshal([]byte(r.Header.Get("CF-Visitor")), &visitor) == nil {
		var scheme string
		if json.Unmarshal(visitor["scheme"], &scheme) == nil && (scheme == "http" || scheme == "https") {
			return scheme
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
