package site

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	legacyPort      = regexp.MustCompile(`:[0-9]+$`)
	legacyScheme    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
	legacyDomain    = regexp.MustCompile(`^[a-z0-9.-]{3,253}$`)
	legacyPHPEntry  = regexp.MustCompile(`(?i)^/([^/]+\.php)(/|$)`)
	legacyPathQuery = regexp.MustCompile(`(?i)(^|&)path=([^&]*)`)
	legacyLanguage  = regexp.MustCompile(`[^a-z0-9_-]`)
)

func legacyTrim(s string) string { return strings.Trim(s, " \t\n\r\x00\x0b") }
func legacyLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// PHP leaves malformed percent sequences unchanged; url.PathUnescape doesn't.
func legacyUnescape(s string, plus bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := url.PathUnescape(s[i : i+3]); err == nil {
				b.WriteString(v)
				i += 2
				continue
			}
		}
		if plus && s[i] == '+' {
			b.WriteByte(' ')
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// These are parse_url components, not net/url components: PHP preserves escape
// bytes, accepts some non-RFC authorities, and replaces literal control bytes.
// Presence matters for empty user/password fields and port zero when hashing.
type legacyURLParts struct {
	scheme, host, user, password, path, query, fragment string
	port                                                int
	hasUser, hasPassword, hasPort                       bool
}

func legacyURLControls(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c < 32 || c == 127 {
			b[i] = '_'
		}
	}
	return string(b)
}

func legacyURLScheme(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("+.-", rune(c))) {
			return false
		}
	}
	return true
}

// PHP's authority port conversion uses a numeric prefix, not a strict integer
// parse (for example //host:80x/ has port 80). Its five-byte bound is separate.
func legacyURLPort(s string) (int, bool) {
	if len(s) > 5 {
		return 0, false
	}
	s = strings.TrimLeft(s, " \t\r\n\v\f")
	i := 0
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	return n, err == nil && n >= 0 && n <= 65535
}

func legacyParseURL(raw string) (p legacyURLParts, ok bool) {
	start, authority := 0, false
	colon := strings.IndexByte(raw, ':')
	portCandidate := false
	if colon > 0 && legacyURLScheme(raw[:colon]) {
		after := raw[colon+1:]
		if after == "" {
			p.scheme = raw[:colon]
			return p, true
		}
		if after[0] != '/' {
			n := 0
			for n < len(after) && after[n] >= '0' && after[n] <= '9' {
				n++
			}
			portCandidate = n < 6 && (n == len(after) || after[n] == '/')
			if !portCandidate {
				p.scheme, start = raw[:colon], colon+1
			}
		} else {
			p.scheme, start = raw[:colon], colon+1
			if strings.HasPrefix(after, "//") {
				start, authority = colon+3, true
				if strings.EqualFold(p.scheme, "file") && strings.HasPrefix(raw[start:], "/") {
					authority = false
					if start+2 < len(raw) && raw[start+2] == ':' {
						start++ // file:///c:/path
					}
				}
			}
		}
	} else {
		boundary := len(raw)
		if i := strings.IndexAny(raw, "?#"); i >= 0 {
			boundary = i
		}
		portCandidate = colon == 0 || colon > 0 && colon+1 < len(raw) && colon < boundary
		if !portCandidate && strings.HasPrefix(raw, "//") {
			start, authority = 2, true
		}
	}
	if portCandidate {
		after := raw[colon+1:]
		n := 0
		for n < len(after) && n < 6 && after[n] >= '0' && after[n] <= '9' {
			n++
		}
		if n > 0 && n < 6 && (n == len(after) || after[n] == '/') {
			var valid bool
			p.port, valid = legacyURLPort(after[:n])
			if !valid {
				return p, false
			}
			p.hasPort, authority = true, true
		} else if after == "" {
			return p, false
		}
		if strings.HasPrefix(raw, "//") {
			start, authority = 2, true
		}
	}
	if authority {
		rest := raw[start:]
		end := strings.IndexAny(rest, "/?#")
		if end < 0 {
			end = len(rest)
		}
		host := rest[:end]
		start += end
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			p.user, p.password, p.hasPassword = strings.Cut(host[:at], ":")
			p.hasUser = true
			host = host[at+1:]
		}
		if !(strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]")) {
			if i := strings.LastIndexByte(host, ':'); i >= 0 {
				if p.port == 0 && host[i+1:] != "" {
					var valid bool
					p.port, valid = legacyURLPort(host[i+1:])
					if !valid {
						return p, false
					}
					p.hasPort = true
				}
				host = host[:i]
			}
		}
		if host == "" {
			return p, false
		}
		p.host = host
	}
	path := raw[start:]
	path, p.fragment, _ = strings.Cut(path, "#")
	p.path, p.query, _ = strings.Cut(path, "?")
	p.scheme, p.host = legacyURLControls(p.scheme), legacyURLControls(p.host)
	p.user, p.password = legacyURLControls(p.user), legacyURLControls(p.password)
	p.path, p.query, p.fragment = legacyURLControls(p.path), legacyURLControls(p.query), legacyURLControls(p.fragment)
	return p, true
}

// parse_url(..., PHP_URL_PATH) ?: '/', before rawurldecode. In particular, do
// not replace control bytes that are introduced only by the later decoding.
func legacyURLPath(raw string) string {
	p, ok := legacyParseURL(raw)
	if !ok || p.path == "" || p.path == "0" {
		return "/"
	}
	return p.path
}

func legacySplitSuffix(s string) (string, string) {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

func NormalizeLegacyPrefix(value string) string {
	value = legacyTrim(legacyUnescape(value, false))
	if strings.HasPrefix(value, "#") {
		return "#"
	}
	value = strings.ReplaceAll(value, `\`, "/")
	value = strings.Trim(value, "/ \t\n\r\x00\x0b")
	// RoutePrefix replaces whitespace before stripping URL separators; the
	// navigation-word helper does the reverse, so they cannot share this step.
	var normalized strings.Builder
	space := false
	for _, c := range value {
		if unicode.IsSpace(c) {
			if !space {
				normalized.WriteByte('-')
			}
			space = true
			continue
		}
		space = false
		if unicode.IsLetter(c) || unicode.IsNumber(c) || unicode.IsMark(c) || strings.ContainsRune("._-", c) {
			normalized.WriteRune(c)
		}
	}
	value = strings.Trim(normalized.String(), "-_ .")
	value = legacyLower(value)
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}

func legacyRouteWord(s string) string {
	var b strings.Builder
	space := false
	for _, c := range s {
		if strings.ContainsRune("/?#&=%", c) {
			continue
		}
		if unicode.IsSpace(c) {
			if !space {
				b.WriteByte('-')
			}
			space = true
			continue
		}
		space = false
		if unicode.IsLetter(c) || unicode.IsNumber(c) || unicode.IsMark(c) || strings.ContainsRune("._-", c) {
			b.WriteRune(c)
		}
	}
	return strings.Trim(b.String(), "-_ .")
}

func legacyPrefixReserved(s string) bool {
	s = legacyLower(legacyTrim(s))
	return s == "" || strings.Contains(s, "link") || s == "admin" || s == "health" || s == "healthz"
}

func ApplyLegacyPrefix(path, prefix string) string  { return legacyPrefixPath(path, prefix, false) }
func RemoveLegacyPrefix(path, prefix string) string { return legacyPrefixPath(path, prefix, true) }
func legacyPrefixPath(path, prefix string, remove bool) string {
	prefix = NormalizeLegacyPrefix(prefix)
	if prefix == "" || path == "" || strings.HasPrefix(path, "//") || strings.HasPrefix(path, "#") || legacyScheme.MatchString(path) {
		return path
	}
	if remove && prefix == "#" {
		return path
	}
	p, suffix := legacySplitSuffix(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	segments := strings.FieldsFunc(p, func(c rune) bool { return c == '/' })
	if prefix == "#" {
		for _, s := range segments {
			if strings.Contains(legacyLower(s), "link") {
				return p + suffix
			}
		}
		return "/#"
	}
	if len(segments) == 0 {
		return p + suffix
	}
	first := segments[0]
	if remove {
		if !strings.HasPrefix(legacyLower(first), prefix) {
			return p + suffix
		}
		rest := first[len(prefix):]
		if legacyPrefixReserved(rest) {
			return p + suffix
		}
		segments[0] = rest
	} else {
		if legacyPrefixReserved(first) {
			return p + suffix
		}
		segments[0] = prefix + first
	}
	result := "/" + strings.Join(segments, "/")
	if strings.HasSuffix(p, "/") {
		result += "/"
	}
	return result + suffix
}

func legacyRawEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func legacyHashURI(raw, prefix string) string {
	if raw == "" || prefix == "" {
		return raw
	}
	p, ok := legacyParseURL(raw)
	if !ok {
		return RemoveLegacyPrefix(raw, prefix)
	}
	p.path = RemoveLegacyPrefix(p.path, prefix)
	p.query = legacyPathQuery.ReplaceAllStringFunc(p.query, func(s string) string {
		m := legacyPathQuery.FindStringSubmatch(s)
		return m[1] + "path=" + legacyRawEncode(RemoveLegacyPrefix(legacyUnescape(m[2], false), prefix))
	})
	// DynamicPageService reconstructs components explicitly, including omitting
	// the leading // for a protocol-relative authority. Match that hash contract.
	var b strings.Builder
	if p.scheme != "" {
		b.WriteString(p.scheme + "://")
	}
	if p.hasUser {
		b.WriteString(p.user)
		if p.hasPassword {
			b.WriteString(":" + p.password)
		}
		b.WriteByte('@')
	}
	b.WriteString(p.host)
	if p.hasPort {
		b.WriteString(":" + strconv.Itoa(p.port))
	}
	b.WriteString(p.path)
	if p.query != "" {
		b.WriteString("?" + p.query)
	}
	if p.fragment != "" {
		b.WriteString("#" + p.fragment)
	}
	return b.String()
}

func legacyQuery(raw string) map[string]string {
	q := make(map[string]string)
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[i+1:]
	} else {
		return q
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	for _, item := range strings.Split(raw, "&") {
		key, value, _ := strings.Cut(item, "=")
		// parse_str decodes the key only once, truncates at NUL, drops
		// leading ASCII spaces, then normalizes root dots/spaces. The
		// EntryParams path value is urldecoded separately, after this step.
		key = legacyUnescape(key, true)
		key, _, _ = strings.Cut(key, "\x00")
		key = strings.TrimLeft(key, " ")
		value = legacyUnescape(value, true)
		if i := strings.IndexByte(key, '['); i >= 0 {
			if i == 0 {
				continue // PHP discards empty root variable names.
			}
			if strings.Contains(key[i+1:], "]") {
				key = key[:i]
				value = "Array" // Public consumers use PHP's string cast.
			} else {
				// An unclosed first bracket is a scalar key, not an array.
				key = strings.ReplaceAll(key, "[", "_")
			}
		}
		key = strings.NewReplacer(".", "_", " ", "_").Replace(key)
		if key != "" {
			q[key] = value
		}
	}
	return q
}

func legacyRequest(r *http.Request, cfg SiteConfig, s LegacySnapshot) LegacyPage {
	p := LegacyPage{RequestHost: r.Host}
	p.RequestURI = r.RequestURI
	if p.RequestURI == "" {
		p.RequestURI = r.URL.RequestURI()
	}
	q := legacyQuery(p.RequestURI)
	p.EntryHost = normalizePHPHost(r.Host)
	if p.EntryHost == "" {
		p.EntryHost = "localhost"
	}
	p.Host = p.EntryHost
	if domain := normalizePHPDomain(q["domain"]); domain != "" {
		p.Host = domain
	}
	p.Scheme = legacyRequestScheme(r)
	path := p.RequestURI
	p.HomeFallback = q["__apk_home_fallback"] == "1"
	scripts := s.EntryScripts
	if scripts == nil {
		scripts = []string{"/index.php", "/799lh.php", "/router.php"}
	}
	if m := legacyPHPEntry.FindStringSubmatch(legacyURLPath(path)); m != nil {
		exists := false
		for _, script := range scripts {
			if script == "/"+m[1] {
				exists = true
				break
			}
		}
		p.HomeFallback = p.HomeFallback || !exists
	}
	p.HashURI = p.RequestURI
	if p.HomeFallback {
		path = "/"
		p.HashURI = "/"
	}
	if q["path"] != "" {
		path = legacyUnescape(q["path"], true)
		if i := strings.IndexByte(path, '#'); i >= 0 {
			path = path[:i]
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	p.NormalizedPath = normalizePHPPath(path)
	p.RoutePrefix = NormalizeLegacyPrefix(cfg.RoutePrefix)
	p.ContentPath = RemoveLegacyPrefix(p.NormalizedPath, p.RoutePrefix)
	p.DisplayPath = ApplyLegacyPrefix(p.ContentPath, p.RoutePrefix)
	p.HashURI = legacyHashURI(p.HashURI, p.RoutePrefix)
	lang, ok := q["lan"]
	if !ok {
		lang = q["lang"]
	}
	lang = legacyLower(legacyTrim(lang))
	if lang != "" && lang != "id" && lang != "en" && lang != "vn" && lang != "th" {
		lang = "en"
	}
	if lang == "" {
		lang = cfg.DefaultLanguage
	}
	if lang == "" {
		lang = "id"
	}
	for _, host := range []string{p.Host, p.EntryHost} {
		bound := ""
		for _, binding := range s.DomainBindings {
			if normalizePHPHost(binding.Domain) == host && legacyTrim(binding.Language) != "" {
				bound = legacyLower(legacyTrim(binding.Language))
				break
			}
		}
		if bound != "" {
			lang = bound
			break
		}
	}
	p.RequestedLanguage = lang
	p.Language = s.LanguageBank.legacyEffectiveLanguage(lang, s.FallbackLanguage, cfg.DefaultLanguage)
	p.Hash = sha1Hex(p.Scheme + "|" + p.Host + "|" + p.Language + "|" + p.EntryHost + "|" + p.HashURI)
	p.PageType = legacyPageTypeNormalized(p.Host, p.ContentPath)
	trigger := s.HubTrigger
	if trigger == "" {
		trigger = "link"
	}
	p.HubRoute = s.HubEnabled && strings.Contains(legacyLower(p.NormalizedPath), legacyLower(trigger))
	return p
}

func (b *LanguageBank) legacyEffectiveLanguage(lang, fallback, def string) string {
	if fallback == "" {
		fallback = "en"
	}
	if def == "" {
		def = "id"
	}
	for _, candidate := range []string{lang, fallback, def} {
		candidate = legacyLanguage.ReplaceAllString(legacyLower(legacyTrim(candidate)), "")
		if candidate == "" {
			candidate = def
		}
		if d, ok := b.languages[candidate]; ok && (len(d.words) > 0 || len(d.sentences) > 0) {
			return candidate
		}
	}
	return "en"
}

func (p LegacyPage) internalURL(path string, seo bool) string {
	path = "/" + strings.Trim(path, "/") + "/"
	if path == "//" {
		path = "/"
	}
	path = ApplyLegacyPrefix(path, p.RoutePrefix)
	if seo {
		path = strings.Trim(path, "/-")
		if path == "" {
			path = "apk"
		}
		return p.SEOBaseURL + "/" + path + "/"
	}
	// Public navigation is deliberately origin-relative.  This keeps links on
	// the port that served the current request (including local preview ports)
	// instead of sending visitors to the canonical/default port.
	return path
}
