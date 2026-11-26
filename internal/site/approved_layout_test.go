package site

import (
	"net/url"
	"sort"
	"strings"
	"testing"
)

// The PHP recordings remain unchanged. Adapt only the user-approved Go UI
// differences: local images, relative same-site navigation, no related-APK
// cards, and no footer LK entry. Multisets still retain every other duplicate.
func approvedLegacyHrefs(t *testing.T, original []string, imageURL, canonical string, p LegacyPage) []string {
	t.Helper()
	counts := map[string]int{}
	for _, href := range original {
		counts[href]++
	}
	remove := func(href string) {
		t.Helper()
		if counts[href] < 1 {
			t.Fatalf("approved removed link %q is missing from PHP recording", href)
		}
		counts[href]--
	}
	for _, link := range p.RelatedLinks {
		remove(link.Path)
	}
	remove(p.Scheme + "://" + p.Host + ApplyLegacyPrefix("/link/", p.RoutePrefix))
	// Managed external links are literal URLs even when they share this host.
	keepAbsolute := map[string]int{}
	for _, link := range p.MainExternalLinks {
		keepAbsolute[link.Path]++
	}
	if p.ShowExternalPanel {
		for _, link := range p.ExternalLinks {
			keepAbsolute[link.Path]++
		}
	}
	if canonical != "" {
		keepAbsolute[canonical]++
	}
	out := []string{}
	for href, count := range counts {
		for i := 0; i < count; i++ {
			switch {
			case imageURL != "" && href == imageURL:
				out = append(out, "<LOCAL_IMAGE>")
			case keepAbsolute[href] > 0:
				out = append(out, href)
				keepAbsolute[href]--
			default:
				out = append(out, expectedRelativeAnchor(href, p.RequestHost, p.EntryHost, p.Host))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Independent expectation adapter; do not call the production href renderer.
// Preserve the original literal tail instead of re-escaping Unicode URLs.
func expectedRelativeAnchor(raw string, hosts ...string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}
	for _, host := range hosts {
		h, err := url.Parse("//" + host)
		if err != nil || !strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(h.Hostname(), ".")) || (u.Port() != "" && u.Port() != h.Port()) {
			continue
		}
		tail := strings.TrimPrefix(strings.TrimPrefix(raw, u.Scheme+":"), "//")
		if i := strings.IndexAny(tail, "/?#"); i >= 0 {
			tail = tail[i:]
		} else {
			tail = "/"
		}
		if !strings.HasPrefix(tail, "/") {
			tail = "/" + tail
		}
		if strings.HasPrefix(tail, "//") || strings.HasPrefix(tail, `/\`) {
			tail = "/." + tail
		}
		return tail
	}
	return raw
}
