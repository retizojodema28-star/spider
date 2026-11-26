package site

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeBankPreservesPHPWhitespaceAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "id")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "word.txt"), []byte("awal\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sentence := "\u00a0A sentence with PHP-significant boundary whitespace.\u00a0"
	if err := os.WriteFile(filepath.Join(dir, "sentences.txt"), []byte(sentence+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := readRuntimeLanguage(context.Background(), root, "id")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := LoadLanguageBank(root, "id")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data.sentences, ordinary.languages["id"].sentences) || data.sentences[0] != sentence {
		t.Fatalf("runtime/worker changed PHP significant text: %#v", data.sentences)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte(sentence), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := readRuntimeLanguage(context.Background(), root, "id"); err == nil {
		t.Fatal("language loader followed symlink")
	}
}
