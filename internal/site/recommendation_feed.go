package site

import (
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
)

const (
	RecommendationFeedURL        = "https://idsite.vip/feed.php"
	recommendationFeedCacheTTL   = 5 * time.Minute
	recommendationFeedTimeout    = 4 * time.Second
	recommendationFeedMaxBytes   = 512 << 10
	recommendationFeedMaxEntries = 256
)

type cachedRecommendation struct {
	body      template.HTML
	expiresAt time.Time
}

// RecommendationFeed pulls the "recommended sites" fragment from the fixed
// application endpoint. Request URLs are never accepted from callers, which
// prevents this feature from becoming a general-purpose SSRF proxy.
type RecommendationFeed struct {
	baseURL *url.URL
	client  *http.Client
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedRecommendation
}

func NewRecommendationFeed(rawURL string, client *http.Client) (*RecommendationFeed, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("recommendation feed URL is empty")
	}
	base, err := url.Parse(rawURL)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("recommendation feed URL must be an absolute HTTP or HTTPS URL without credentials")
	}
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	clientCopy := *client
	client = &clientCopy
	feed := &RecommendationFeed{
		baseURL: base,
		client:  client,
		now:     time.Now,
		cache:   map[string]cachedRecommendation{},
	}
	previousCheck := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many recommendation feed redirects")
		}
		if !strings.EqualFold(req.URL.Scheme, base.Scheme) || !strings.EqualFold(req.URL.Host, base.Host) {
			return errors.New("recommendation feed redirected outside the configured origin")
		}
		if previousCheck != nil {
			return previousCheck(req, via)
		}
		return nil
	}
	return feed, nil
}

// Fetch asks the configured feed for the current content domain and original
// request path. The returned fragment is parsed and rebuilt with an allowlist,
// then cached briefly so public requests do not depend on upstream latency.
func (f *RecommendationFeed) Fetch(ctx context.Context, domain, requestPath string) (template.HTML, error) {
	if f == nil {
		return "", nil
	}
	domain = strings.TrimSpace(domain)
	requestPath = strings.TrimSpace(requestPath)
	if domain == "" {
		return "", errors.New("recommendation feed domain is empty")
	}
	if requestPath == "" || !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	cacheKey := strings.ToLower(domain) + "\x00" + requestPath
	now := f.now()
	f.mu.Lock()
	if cached, ok := f.cache[cacheKey]; ok && now.Before(cached.expiresAt) {
		f.mu.Unlock()
		return cached.body, nil
	}
	f.mu.Unlock()

	target := *f.baseURL
	query := target.Query()
	query.Set("domain", domain)
	query.Set("path", requestPath)
	target.RawQuery = query.Encode()

	fetchCtx, cancel := context.WithTimeout(ctx, recommendationFeedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build recommendation feed request")
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "apk-html-go-recommendation-feed/1.0")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request recommendation feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("recommendation feed returned HTTP %d", resp.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType != "" && contentType != "text/html" && contentType != "application/xhtml+xml" {
		return "", fmt.Errorf("recommendation feed returned unsupported content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, recommendationFeedMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read recommendation feed: %w", err)
	}
	if len(body) > recommendationFeedMaxBytes {
		return "", errors.New("recommendation feed response is too large")
	}
	safe, err := sanitizeRecommendationFragment(string(body))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(safe)) == "" {
		return "", errors.New("recommendation feed returned no usable content")
	}
	f.mu.Lock()
	if len(f.cache) >= recommendationFeedMaxEntries {
		for key, cached := range f.cache {
			if !now.Before(cached.expiresAt) {
				delete(f.cache, key)
			}
		}
		for key := range f.cache {
			if len(f.cache) < recommendationFeedMaxEntries {
				break
			}
			delete(f.cache, key)
		}
	}
	f.cache[cacheKey] = cachedRecommendation{body: safe, expiresAt: now.Add(recommendationFeedCacheTTL)}
	f.mu.Unlock()
	return safe, nil
}

var recommendationAllowedTags = map[string]bool{
	"a": true, "b": true, "br": true, "div": true, "em": true, "h2": true, "h3": true,
	"h4": true, "i": true, "li": true, "ol": true, "p": true, "section": true,
	"span": true, "strong": true, "ul": true,
}

func sanitizeRecommendationFragment(raw string) (template.HTML, error) {
	doc, err := xhtml.Parse(strings.NewReader("<div id=\"recommendation-feed-root\">" + raw + "</div>"))
	if err != nil {
		return "", errors.New("parse recommendation feed HTML")
	}
	root := findRecommendationRoot(doc)
	if root == nil {
		return "", errors.New("recommendation feed has no content root")
	}
	var out strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		writeSafeRecommendationNode(&out, child)
	}
	return template.HTML(out.String()), nil
}

func findRecommendationRoot(n *xhtml.Node) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == "div" {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == "recommendation-feed-root" {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findRecommendationRoot(child); found != nil {
			return found
		}
	}
	return nil
}

func writeSafeRecommendationNode(out *strings.Builder, n *xhtml.Node) {
	switch n.Type {
	case xhtml.TextNode:
		out.WriteString(html.EscapeString(n.Data))
		return
	case xhtml.ElementNode:
		tag := strings.ToLower(n.Data)
		if tag == "script" || tag == "style" || tag == "iframe" || tag == "object" || tag == "embed" {
			return
		}
		if !recommendationAllowedTags[tag] {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				writeSafeRecommendationNode(out, child)
			}
			return
		}
		out.WriteByte('<')
		out.WriteString(tag)
		if tag == "a" {
			href, title := "", ""
			for _, attr := range n.Attr {
				switch strings.ToLower(attr.Key) {
				case "href":
					href = strings.TrimSpace(attr.Val)
				case "title":
					title = attr.Val
				}
			}
			if safeRecommendationHref(href) {
				out.WriteString(` href="`)
				out.WriteString(html.EscapeString(href))
				out.WriteByte('"')
				if title != "" {
					out.WriteString(` title="`)
					out.WriteString(html.EscapeString(title))
					out.WriteByte('"')
				}
				out.WriteString(` target="_blank" rel="noopener noreferrer"`)
			}
		}
		out.WriteByte('>')
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			writeSafeRecommendationNode(out, child)
		}
		out.WriteString("</")
		out.WriteString(tag)
		out.WriteByte('>')
	default:
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			writeSafeRecommendationNode(out, child)
		}
	}
}

func safeRecommendationHref(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	if u.IsAbs() {
		return u.Scheme == "http" || u.Scheme == "https"
	}
	return strings.HasPrefix(raw, "/")
}

func recommendationFeedEnabled(r *http.Request) bool {
	if r == nil {
		return false
	}
	raw := r.RequestURI
	if raw == "" {
		raw = r.URL.RequestURI()
	}
	value := strings.ToLower(strings.TrimSpace(legacyQuery(raw)["feed"]))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func recommendationRequestPath(r *http.Request) string {
	if r == nil {
		return "/"
	}
	raw := r.RequestURI
	if raw == "" {
		raw = r.URL.RequestURI()
	}
	path := ""
	if override := legacyQuery(raw)["path"]; override != "" {
		path = legacyUnescape(override, true)
	} else {
		path = r.URL.Path
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func recommendationHeading(lang string) string {
	switch lang {
	case "id":
		return "Situs Rekomendasi"
	case "th":
		return "เว็บไซต์แนะนำ"
	case "vn":
		return "Trang đề xuất"
	default:
		return "Recommended Sites"
	}
}
