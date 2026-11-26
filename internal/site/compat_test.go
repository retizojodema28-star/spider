package site

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPHPURLCompatibility(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "fold case collapse and resolve dot segments", in: "/A//B/../C/?utm_source=x#top", want: "/a/c"},
		{name: "decode escaped path", in: "/Category%2FGame/", want: "/category/game"},
		{name: "root", in: "/", want: "/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizePHPPath(test.in); got != test.want {
				t.Fatalf("normalizePHPPath(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}

	if got := normalizePHPHost("Example.COM:18443"); got != "example.com" {
		t.Fatalf("normalizePHPHost() = %q, want example.com", got)
	}
	if got := phpCanonicalURL("preview.example.test:18443", "/Apps/Sample-App-One/"); got != "https://preview.example.test/apps/sample-app-one" {
		t.Fatalf("phpCanonicalURL() = %q", got)
	}
}

func TestPHPIndonesianLanguageBankOracle(t *testing.T) {
	bank, err := LoadLanguageBank(filepath.Join("..", "..", "data", "lan"), "id")
	if err != nil {
		t.Fatal(err)
	}
	const bankHash = "0123456789abcdef0123456789abcdef01234567"
	words := bank.Words("id", bankHash, 8, 0)
	if len(words) != 8 || !reflect.DeepEqual(words, bank.Words("id", bankHash, 8, 0)) {
		t.Fatalf("word-bank selection is not stable: %#v", words)
	}
	for _, word := range words {
		if strings.TrimSpace(word) == "" {
			t.Fatal("word-bank selection contains an empty value")
		}
	}
	sentences := bank.Sentences("id", bankHash, 8, 0)
	if len(sentences) != 8 || !reflect.DeepEqual(sentences, bank.Sentences("id", bankHash, 8, 0)) {
		t.Fatalf("sentence-bank selection is not stable: %#v", sentences)
	}
	for _, sentence := range sentences {
		if strings.TrimSpace(sentence) == "" {
			t.Fatal("sentence-bank selection contains an empty value")
		}
	}
	routeHash := phpRouteHash("http", "preview.example.test:18443", "id", "/apps/sample-app-one?lan=id")
	if len(routeHash) != 40 || routeHash != phpRouteHash("http", "preview.example.test:18443", "id", "/apps/sample-app-one?lan=id") {
		t.Fatalf("route hash is not stable: %s", routeHash)
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestPHPKeywordExposureOracle(t *testing.T) {
	words := []string{"sample", "catalog", "studio", "reader", "tool"}
	got := phpKeywordChips(words, "0123456789abcdef0123456789abcdef01234567", words[0])
	if len(got) < 10 || len(got) > 20 || got[0] != "sample APK" || !strings.Contains(got[len(got)-1], "sample vs ") {
		t.Fatalf("keyword exposure differs: count=%d first=%q last=%q", len(got), first(got), got[len(got)-1])
	}
}

func TestPHPPageTypeCompatibility(t *testing.T) {
	tests := []struct {
		host string
		path string
		want string
	}{
		{host: "preview.example.test:18443", path: "/category/sample/", want: "subdomain"},
		{host: "example.com", path: "/category/game/", want: "detail"},
		{host: "example.com", path: "/category/", want: "list"},
		{host: "example.com", path: "/go/app", want: "external"},
		{host: "127.0.0.1:18082", path: "/apps", want: "list"},
	}
	for _, test := range tests {
		if got := phpPageType(test.host, test.path); got != test.want {
			t.Errorf("phpPageType(%q, %q) = %q, want %q", test.host, test.path, got, test.want)
		}
	}
}
