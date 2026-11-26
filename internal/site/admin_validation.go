package site

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Images are served by the parent's /assets mount. A binding must point at a
// real directory within ASSET_DIR containing indexed local image assets.
func (r *Runtime) validateImageBase(raw string) error {
	if len(raw) > 255 || strings.Contains(raw, "%") || validateAdminPath(raw, false) != nil || (raw != "/assets" && !strings.HasPrefix(raw, "/assets/")) {
		return fmt.Errorf("image base must be a local /assets directory")
	}
	root := r.opts.AssetRoot
	if root == "" {
		root = os.Getenv("ASSET_DIR")
	}
	if root == "" {
		root = "assets"
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("ASSET_DIR unavailable")
	}
	base := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(raw, "/assets")))
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("image directory does not exist")
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return fmt.Errorf("image directory escapes ASSET_DIR")
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return fmt.Errorf("image base is not a directory")
	}
	// Rendering checks the indexed base/{brand}/1..5 files and explicitly
	// falls back to original fixtures. A brand directory needs no sample copies.
	files, err := IndexLegacyLocalImageFiles(root)
	if err != nil {
		return fmt.Errorf("unable to index image directory")
	}
	prefix := strings.TrimSuffix(raw, "/") + "/"
	for file := range files {
		if strings.HasPrefix(file, prefix) {
			return nil
		}
	}
	return fmt.Errorf("image directory contains no supported local images")
}

var languagePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
var siteKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,120}$`)
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,179}$`)
var domainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateText(s string, max int) error {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || strings.ContainsAny(s, "\x00\r\n") {
		return fmt.Errorf("invalid text (maximum %d characters)", max)
	}
	return nil
}

func normalizeAdminDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(d, "://") {
		u, err := url.Parse(d)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
			return "", fmt.Errorf("invalid domain")
		}
		d = u.Host
	}
	if host, port, err := net.SplitHostPort(d); err == nil {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid domain port")
		}
		d = host
	}
	d = strings.TrimSuffix(strings.Trim(d, "[]"), ".")
	if net.ParseIP(d) != nil {
		return d, nil
	}
	if len(d) == 0 || len(d) > 253 {
		return "", fmt.Errorf("invalid domain")
	}
	for _, label := range strings.Split(d, ".") {
		if !domainLabelPattern.MatchString(label) {
			return "", fmt.Errorf("invalid domain")
		}
	}
	return d, nil
}

func validateAdminPath(raw string, empty bool) error {
	if raw == "" && empty {
		return nil
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\x00\r\n\t?#") || len(raw) > 1000 {
		return fmt.Errorf("expected a local absolute path")
	}
	decoded := raw
	for i := 0; i < 4; i++ {
		v, err := url.PathUnescape(decoded)
		if err != nil {
			return fmt.Errorf("invalid path encoding")
		}
		if v == decoded {
			break
		}
		decoded = v
	}
	if strings.ContainsAny(decoded, "\\\x00\r\n\t?#%") || strings.HasPrefix(decoded, "//") {
		return fmt.Errorf("unsafe path")
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == ".." || part == "." {
			return fmt.Errorf("path traversal is not allowed")
		}
	}
	if path.Clean(decoded) != strings.TrimSuffix(decoded, "/") && decoded != "/" {
		return fmt.Errorf("path must be normalized")
	}
	return nil
}

func validateAdminURL(raw string, local, fragment bool) error {
	if fragment && raw == "#apk-info" {
		return nil
	}
	if local && strings.HasPrefix(raw, "/") {
		return validateAdminPath(raw, false)
	}
	if err := validateText(raw, 1000); err != nil {
		return err
	}
	if strings.ContainsAny(raw, "\\ \t") {
		return fmt.Errorf("invalid URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("URL must use http or https")
	}
	if _, err = normalizeAdminDomain(u.Host); err != nil {
		return err
	}
	if u.Path != "" {
		if err = validateAdminPath(u.EscapedPath(), false); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) validLanguage(lang string) error {
	for _, supported := range r.opts.Languages {
		if lang == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported language %q", lang)
}

func (r *Runtime) validSite(key string) error {
	if !siteKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid site_key")
	}
	if key == "default" {
		return nil
	}
	for _, s := range r.Snapshot().Configs {
		if s.SiteKey == key {
			return nil
		}
	}
	return fmt.Errorf("unknown site_key")
}
