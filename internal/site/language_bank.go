package site

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LanguageBank struct {
	languages map[string]languageData
}

type languageData struct {
	words     []string
	sentences []string
}

func LoadLanguageBank(root string, languages ...string) (*LanguageBank, error) {
	bank := &LanguageBank{languages: make(map[string]languageData, len(languages))}
	for _, language := range languages {
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "" {
			continue
		}
		data, err := loadLanguageData(filepath.Join(root, language))
		if err != nil {
			return nil, fmt.Errorf("load %s language bank: %w", language, err)
		}
		bank.languages[language] = data
	}
	if len(bank.languages) == 0 {
		return nil, fmt.Errorf("no language banks loaded")
	}
	return bank, nil
}

func loadLanguageData(root string) (languageData, error) {
	wordBytes, err := os.ReadFile(filepath.Join(root, "word.txt"))
	if err != nil {
		return languageData{}, err
	}
	words := uniqueLines(string(wordBytes), true)

	var sentenceFiles []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.EqualFold(entry.Name(), "word.txt") || strings.ToLower(filepath.Ext(entry.Name())) != ".txt" {
			return nil
		}
		sentenceFiles = append(sentenceFiles, path)
		return nil
	})
	if err != nil {
		return languageData{}, err
	}
	sort.Strings(sentenceFiles)

	var rawSentences []string
	for _, file := range sentenceFiles {
		contents, err := os.ReadFile(file)
		if err != nil {
			return languageData{}, err
		}
		for _, line := range strings.Split(string(contents), "\n") {
			line = legacyTrim(line)
			if len(line) < 24 {
				continue
			}
			rawSentences = append(rawSentences, line)
			if len(rawSentences) >= 20000 {
				break
			}
		}
		if len(rawSentences) >= 20000 {
			break
		}
	}
	return languageData{words: words, sentences: uniqueLines(strings.Join(rawSentences, "\n"), true)}, nil
}

func (b *LanguageBank) Words(language, hash string, count, offset int) []string {
	data, ok := b.languages[strings.ToLower(strings.TrimSpace(language))]
	if !ok || len(data.words) == 0 || count <= 0 {
		return nil
	}
	return selectSequential(data.words, hash, count, offset, "language")
}

func (b *LanguageBank) Sentences(language, hash string, count, offset int) []string {
	data, ok := b.languages[strings.ToLower(strings.TrimSpace(language))]
	if !ok || len(data.sentences) == 0 || count <= 0 {
		return nil
	}
	if len(data.sentences) <= 3 || min(count, len(data.sentences)) >= len(data.sentences) {
		return selectSequential(data.sentences, hash, count, offset, "language")
	}

	normalized := normalizeHash(hash)
	seed := sha1Hex(fmt.Sprintf("%s|sentence-segments|%d", normalized, offset))
	total := len(data.sentences)
	start := int(hashUint32(seed[:8]) % uint32(total))
	step := stride(total, seed)
	wanted := min(count, total)
	selected := make([]string, 0, wanted)
	seen := make(map[int]struct{}, wanted)
	for i, attempts := 0, 0; len(selected) < wanted && attempts < total*2; i, attempts = i+1, attempts+1 {
		index := (start + i*step) % total
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		selected = append(selected, data.sentences[index])
	}
	for i := 0; len(selected) < wanted && i < total; i++ {
		index := (start + i) % total
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		selected = append(selected, data.sentences[index])
	}
	return selected
}

func selectSequential(items []string, hash string, count, offset int, separator string) []string {
	if count <= 0 || len(items) == 0 {
		return nil
	}
	normalized := normalizeHash(hash)
	startHex := normalized
	start := max(0, offset*8)
	if start+8 > len(startHex) {
		startHex = sha1Hex(fmt.Sprintf("%s|%s|%d", normalized, separator, offset))
		start = 0
	}
	index := int(hashUint32(startHex[start:start+8]) % uint32(len(items)))
	result := make([]string, min(count, len(items)))
	for i := range result {
		result[i] = items[(index+i)%len(items)]
	}
	return result
}

func uniqueLines(contents string, caseInsensitive bool) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, line := range strings.Split(contents, "\n") {
		line = legacyTrim(line)
		if line == "" {
			continue
		}
		key := line
		if caseInsensitive {
			key = legacyLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, line)
	}
	return result
}

func normalizeHash(value string) string {
	filtered := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F' {
			filtered = append(filtered, byte(strings.ToLower(string(ch))[0]))
		}
	}
	if len(filtered) == 0 {
		return sha1Hex("empty-language-content-hash")
	}
	return string(filtered)
}

func sha1Hex(value string) string {
	digest := sha1.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}

func hashUint32(value string) uint32 {
	decoded, _ := hex.DecodeString(value[:8])
	return uint32(decoded[0])<<24 | uint32(decoded[1])<<16 | uint32(decoded[2])<<8 | uint32(decoded[3])
}

func stride(total int, seed string) int {
	if total <= 4 {
		return 1
	}
	step := 2 + int(hashUint32(seed[8:16])%uint32(max(1, total-3)))
	for i := 0; i < total; i++ {
		if step > 1 && step < total-1 && gcd(step, total) == 1 {
			return step
		}
		step++
		if step >= total-1 {
			step = 2
		}
	}
	return max(2, total/2)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}
