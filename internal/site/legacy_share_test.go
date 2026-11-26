package site

import (
	"net/http/httptest"
	"testing"
)

func TestLegacyShareURLKeepsRequestPort(t *testing.T) {
	bank := &LanguageBank{languages: map[string]languageData{
		"id": {
			words: []string{"Lucky", "Slot", "Android"},
			sentences: []string{
				"Halaman ini menyediakan informasi aplikasi Android yang stabil.",
				"Detail aplikasi disusun dari data lokal yang sudah dimuat.",
				"Navigasi halaman tetap menggunakan alamat yang sedang dibuka.",
				"Informasi versi dan gambar ditampilkan secara konsisten.",
				"Pengunjung dapat membuka bagian informasi APK dengan mudah.",
				"Tautan terkait ditampilkan terpisah dari kata kunci utama.",
			},
		},
	}}
	req := httptest.NewRequest("GET", "http://127.0.0.1:18082/apps/falcon-squad?lan=id", nil)
	req.RequestURI = "/apps/falcon-squad?lan=id"
	page, err := BuildLegacyPage(req, SiteConfig{DefaultLanguage: "id"}, LegacySnapshot{LanguageBank: bank})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := page.RawCanonical, "http://127.0.0.1:18082/apps/falcon-squad"; got != want {
		t.Fatalf("share URL = %q, want %q", got, want)
	}
	if got, want := page.Canonical, "https://127.0.0.1/apps/falcon-squad"; got != want {
		t.Fatalf("SEO canonical = %q, want %q", got, want)
	}
	if got, want := string(page.Href(page.DisplayPath)), `href="/apps/falcon-squad"`; got != want {
		t.Fatalf("share href = %q, want %q", got, want)
	}
}
