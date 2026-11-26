package site

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Files are deployment inputs, never supplied by a request. Reject symlinks,
// special files and oversized input before constructing a replacement bank.
func readRuntimeLanguage(ctx context.Context, root, lang string) (languageData, error) {
	if !languagePattern.MatchString(lang) {
		return languageData{}, fmt.Errorf("invalid language")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return languageData{}, err
	}
	dir := filepath.Join(base, lang)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return languageData{}, fmt.Errorf("language %s directory missing or unsafe", lang)
	}
	var files []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("language %s contains a symlink", lang)
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".txt") {
			if !d.Type().IsRegular() {
				return fmt.Errorf("language input is not a regular file")
			}
			files = append(files, p)
			if len(files) > 10000 {
				return fmt.Errorf("too many language files")
			}
		}
		return nil
	})
	if err != nil {
		return languageData{}, err
	}
	sort.Strings(files)
	read := func(p string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := openRuntimeRegularFile(p)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
		if err != nil {
			return "", err
		}
		if len(b) > 8<<20 {
			return "", fmt.Errorf("language file exceeds 8 MiB")
		}
		if !utf8.Valid(b) {
			return "", fmt.Errorf("language file is not UTF-8")
		}
		return string(b), nil
	}
	wordText, err := read(filepath.Join(dir, "word.txt"))
	if err != nil {
		return languageData{}, fmt.Errorf("language %s words: %w", lang, err)
	}
	data := languageData{words: uniqueLines(wordText, true)}
	total := len(wordText)
	var lines []string
	for _, p := range files {
		if strings.EqualFold(filepath.Base(p), "word.txt") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return languageData{}, err
		}
		text, err := read(p)
		if err != nil {
			return languageData{}, fmt.Errorf("language %s sentences: %w", lang, err)
		}
		total += len(text)
		if total > 64<<20 {
			return languageData{}, fmt.Errorf("language %s exceeds 64 MiB", lang)
		}
		for _, line := range strings.Split(text, "\n") {
			line = legacyTrim(line)
			if len(line) >= 24 {
				lines = append(lines, line)
			}
			if len(lines) >= 20000 {
				break
			}
		}
		if len(lines) >= 20000 {
			break
		}
	}
	data.sentences = uniqueLines(strings.Join(lines, "\n"), true)
	if len(data.words) == 0 || len(data.sentences) == 0 {
		return languageData{}, fmt.Errorf("language %s requires nonempty words and sentences", lang)
	}
	return data, nil
}
