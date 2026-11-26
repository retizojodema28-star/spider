package site

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// HubConfig is the normalized PHP HubConfigRepository contract. Store this in
// MySQL; a nil *HubConfig in HubSnapshot means the enabled-by-default config.
type HubConfig struct {
	Enabled            bool   `json:"enabled"`
	TriggerKeyword     string `json:"trigger_keyword"`
	SubdomainDSL       string `json:"subdomain_dsl"`
	SubdomainCount     int    `json:"subdomain_count"`
	ExternalMin        int    `json:"external_min"`
	ExternalMax        int    `json:"external_max"`
	BottomLinkCount    int    `json:"bottom_link_count"`
	DateNavNumMax      int    `json:"date_nav_num_max"`
	LinkRel            string `json:"link_rel"`
	IntroMinWords      int    `json:"intro_min_words"`
	ShowListPanel      bool   `json:"show_list_panel"`
	ShowSubdomainPanel bool   `json:"show_subdomain_panel"`
	ShowExternalPanel  bool   `json:"show_external_panel"`
}

func DefaultHubConfig() HubConfig {
	return HubConfig{Enabled: true, TriggerKeyword: "link", SubdomainDSL: "{s10n10}", SubdomainCount: 20, ExternalMin: 80, ExternalMax: 120, BottomLinkCount: 6, DateNavNumMax: 2000, IntroMinWords: 220}
}

var hubTriggerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
var hubDSLPattern = regexp.MustCompile(`^(\{([sn][0-9]+)+\})+$`)
var hubDSLSegment = regexp.MustCompile(`([sn])([0-9]+)`)
var hubRelInvalid = regexp.MustCompile(`[^a-z\s]`)
var hubSpace = regexp.MustCompile(`\s+`)

// DecodeHubConfig preserves explicit false/zero while defaulting missing or
// malformed config, including PHP's loose boolean/integer normalization.
func DecodeHubConfig(raw []byte) HubConfig {
	c := DefaultHubConfig()
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&m) != nil || m == nil || !json.Valid(raw) {
		return c
	}
	for key, dst := range map[string]*bool{"enabled": &c.Enabled, "show_list_panel": &c.ShowListPanel, "show_subdomain_panel": &c.ShowSubdomainPanel, "show_external_panel": &c.ShowExternalPanel} {
		if v, ok := m[key]; ok && v != nil {
			*dst = hubBool(v)
		}
	}
	for key, dst := range map[string]*int{"subdomain_count": &c.SubdomainCount, "external_min": &c.ExternalMin, "external_max": &c.ExternalMax, "bottom_link_count": &c.BottomLinkCount, "date_nav_num_max": &c.DateNavNumMax, "intro_min_words": &c.IntroMinWords} {
		if v, ok := m[key]; ok && v != nil {
			*dst = hubInt(v)
		}
	}
	for key, dst := range map[string]*string{"trigger_keyword": &c.TriggerKeyword, "subdomain_dsl": &c.SubdomainDSL, "link_rel": &c.LinkRel} {
		if v, ok := m[key]; ok && v != nil {
			*dst = hubString(v)
		}
	}
	return c.Normalize()
}

func hubBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	if n, ok := v.(json.Number); ok {
		f, err := n.Float64()
		return err == nil && f == 1
	}
	s := strings.ToLower(strings.TrimSpace(hubString(v)))
	return s == "1" || s == "true" || s == "on" || s == "yes"
}

var hubIntPrefix = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)

func hubInt(v any) int {
	if b, ok := v.(bool); ok {
		if b {
			return 1
		}
		return 0
	}
	s := hubIntPrefix.FindString(strings.TrimSpace(hubString(v)))
	n, _ := strconv.ParseFloat(s, 64)
	return int(n)
}
func hubString(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(v)
}

func (c HubConfig) Normalize() HubConfig {
	c.TriggerKeyword = legacyLower(legacyTrim(c.TriggerKeyword))
	if !hubTriggerPattern.MatchString(c.TriggerKeyword) {
		c.TriggerKeyword = "link"
	}
	c.SubdomainDSL = legacyTrim(c.SubdomainDSL)
	if !hubDSLPattern.MatchString(c.SubdomainDSL) {
		c.SubdomainDSL = "{s10n10}"
	}
	c.SubdomainCount = max(1, min(200, c.SubdomainCount))
	c.ExternalMin = max(1, c.ExternalMin)
	c.ExternalMax = max(c.ExternalMin, c.ExternalMax)
	c.BottomLinkCount = max(0, min(50, c.BottomLinkCount))
	c.DateNavNumMax = max(10, min(9999, c.DateNavNumMax))
	c.IntroMinWords = max(50, c.IntroMinWords)
	parts := hubSpace.Split(legacyTrim(hubRelInvalid.ReplaceAllString(legacyLower(c.LinkRel), "")), -1)
	seen := map[string]bool{"": true, "nofollow": true, "ugc": true, "sponsored": true, "noopener": true, "noreferrer": true}
	keep := []string{}
	for _, p := range parts {
		if !seen[p] {
			seen[p] = true
			keep = append(keep, p)
		}
	}
	c.LinkRel = strings.Join(keep, " ")
	return c
}

func hubConfig(config *HubConfig) HubConfig {
	if config == nil {
		return DefaultHubConfig()
	}
	return config.Normalize()
}

func ShouldHandleHub(normalizedPath string, config *HubConfig) bool {
	c := hubConfig(config)
	return c.Enabled && strings.Contains(legacyLower(normalizedPath), c.TriggerKeyword)
}

type HubDSLSegment struct {
	Type   string `json:"type"`
	Length int    `json:"length"`
}

func ParseHubDSL(dsl string) []HubDSLSegment {
	dsl = legacyTrim(dsl)
	if !hubDSLPattern.MatchString(dsl) {
		return nil
	}
	result := []HubDSLSegment{}
	for _, m := range hubDSLSegment.FindAllStringSubmatch(dsl, -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil || n < 1 || n > 32 {
			return nil
		}
		result = append(result, HubDSLSegment{m[1], n})
	}
	return result
}

// GenerateHubSubdomains is for an explicit admin/worker refresh, never render.
// PHP refresh uses secure randomness; deterministic navigation rotates the
// persisted pool without sorting or generating missing entries.
func GenerateHubSubdomains(dsl string, count int) ([]string, error) {
	segments := ParseHubDSL(dsl)
	if len(segments) == 0 {
		return nil, errors.New("subdomain_dsl is invalid")
	}
	count = max(1, min(200, count))
	result := []string{}
	seen := map[string]bool{}
	for attempt := 0; len(result) < count && attempt < 5000; attempt++ {
		var b strings.Builder
		for _, s := range segments {
			pool := "abcdefghijklmnopqrstuvwxyz"
			if s.Type == "n" {
				pool = "0123456789"
			}
			for i := 0; i < s.Length; i++ {
				n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
				if err != nil {
					return nil, err
				}
				b.WriteByte(pool[n.Int64()])
			}
		}
		prefix := b.String()
		if !seen[prefix] {
			seen[prefix] = true
			result = append(result, prefix)
		}
	}
	if len(result) < count {
		return nil, fmt.Errorf("subdomain DSL namespace exhausted: %d/%d", len(result), count)
	}
	return result, nil
}

func NormalizeHubDomain(domain string) string {
	return normalizePHPDomain(domain)
}
