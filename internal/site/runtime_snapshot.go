package site

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RuntimeOptions struct {
	LanguageRoot string
	AssetRoot    string
	Languages    []string
	PollInterval time.Duration
	OnError      func(error)
}

// Snapshot and everything reachable from it are read-only after publication.
// Retain one Snapshot for an entire HTTP request to avoid mixing revisions.
type Snapshot struct {
	Hub                   HubSnapshot
	LinkMetadata          map[uint]RuntimeLinkMetadata
	LocalImageFiles       map[string]bool
	Revision              uint64
	LoadedAt              time.Time
	Config                SiteConfig
	Configs               []SiteConfig
	Apps                  []App
	Links                 []SiteLink
	DomainBindings        []DomainBinding
	ImageBindings         []DomainImageBinding
	InternalDomains       []InternalDomain
	MainPageExternalLinks []SiteLink
	Bank                  *LanguageBank
	Languages             []string
}

type Runtime struct {
	db       *gorm.DB
	opts     RuntimeOptions
	current  atomic.Pointer[Snapshot]
	reloadMu sync.Mutex
}

func runtimeLanguages(langs []string) ([]string, error) {
	out := []string{"id"}
	seen := map[string]bool{"id": true}
	for _, lang := range langs {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if !languagePattern.MatchString(lang) {
			return nil, fmt.Errorf("invalid language %q", lang)
		}
		if !seen[lang] {
			out = append(out, lang)
			seen[lang] = true
		}
	}
	return out, nil
}

func NewRuntime(ctx context.Context, db *gorm.DB, opts RuntimeOptions) (*Runtime, error) {
	if db == nil {
		return nil, fmt.Errorf("runtime database is required")
	}
	langs, err := runtimeLanguages(opts.Languages)
	if err != nil {
		return nil, err
	}
	opts.Languages = langs
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	r := &Runtime{db: db, opts: opts}
	if err := r.Reload(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) Snapshot() *Snapshot { return r.current.Load() }

// Reload builds a complete replacement. A failed read preserves the old snapshot.
func (r *Runtime) Reload(ctx context.Context) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	s := &Snapshot{Languages: append([]string(nil), r.opts.Languages...)}
	var versions []LanguageVersion
	var metadata []RuntimeLinkMetadata
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rev RuntimeRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, 1).Error; err != nil {
			return err
		}
		s.Revision = rev.Revision
		for _, q := range []*gorm.DB{
			tx.Order("site_key asc").Find(&s.Configs),
			tx.Preload("Screenshots", func(db *gorm.DB) *gorm.DB { return db.Order("position asc") }).Order("name asc, id asc").Find(&s.Apps),
			tx.Order("weight desc, id asc").Find(&s.Links),
			tx.Order("domain asc").Find(&s.DomainBindings),
			tx.Order("domain asc").Find(&s.ImageBindings),
			tx.Order("weight desc, id asc").Find(&s.InternalDomains),
			tx.Order("language asc").Find(&versions),
			tx.Order("link_id asc").Find(&metadata),
		} {
			if q.Error != nil {
				return q.Error
			}
		}
		var err error
		s.Hub, err = loadRuntimeHub(tx, s.Links)
		return err
	})
	if err != nil {
		return fmt.Errorf("load runtime snapshot: %w", err)
	}
	s.LinkMetadata = map[uint]RuntimeLinkMetadata{}
	for _, m := range metadata {
		s.LinkMetadata[m.LinkID] = m
	}
	s.Config = SiteConfig{SiteKey: "default", DisplayName: "APK Hub", DefaultLanguage: "id", RoutePrefix: "", DownloadBaseURL: "https://download.invalid/apk/", ImageBasePath: "/assets", ExternalLinkLimit: 12, MaxRelatedLinks: 6}
	for _, cfg := range s.Configs {
		if cfg.SiteKey == "default" {
			s.Config = cfg
		}
	}
	if s.Config.DefaultLanguage == "" {
		s.Config.DefaultLanguage = "id"
	}
	for _, link := range s.Links {
		if link.Kind == "main-external" {
			s.MainPageExternalLinks = append(s.MainPageExternalLinks, link)
		}
	}
	// Reuse the previous immutable bank when only unrelated data changed.
	bank := &LanguageBank{languages: map[string]languageData{}}
	for _, v := range versions {
		data, err := decodeRuntimeBank(v.Payload)
		if err != nil {
			return fmt.Errorf("decode active bank %s: %w", v.Language, err)
		}
		bank.languages[v.Language] = data
	}
	for _, lang := range s.Languages {
		if _, ok := bank.languages[lang]; ok {
			continue
		}
		if old := r.Snapshot(); old != nil && old.Bank != nil {
			if data, ok := old.Bank.languages[lang]; ok {
				bank.languages[lang] = data
				continue
			}
		}
		if r.opts.LanguageRoot == "" {
			continue
		}
		data, err := readRuntimeLanguage(ctx, r.opts.LanguageRoot, lang)
		if err != nil {
			return err
		}
		bank.languages[lang] = data
	}
	if r.opts.AssetRoot != "" {
		files, err := IndexLegacyLocalImageFiles(r.opts.AssetRoot)
		if err != nil {
			return fmt.Errorf("index local images: %w", err)
		}
		s.LocalImageFiles = files
	}
	s.Bank = bank
	s.LoadedAt = time.Now().UTC()
	r.current.Store(s)
	return nil
}

// ReloadIfChanged performs one cheap revision query; no public request calls it.
func (r *Runtime) ReloadIfChanged(ctx context.Context) error {
	var rev RuntimeRevision
	if err := r.db.WithContext(ctx).First(&rev, 1).Error; err != nil {
		return err
	}
	if old := r.Snapshot(); old != nil && old.Revision == rev.Revision {
		return nil
	}
	return r.Reload(ctx)
}

func (r *Runtime) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.ReloadIfChanged(ctx); err != nil && r.opts.OnError != nil {
				r.opts.OnError(err)
			}
		}
	}
}

func snapshotHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
}

func (s *Snapshot) ConfigForHost(host string) SiteConfig {
	cfg := s.Config
	host = snapshotHost(host)
	for _, b := range s.DomainBindings {
		if b.Domain == host {
			for _, v := range s.Configs {
				if v.SiteKey == b.SiteKey {
					cfg = v
					break
				}
			}
			cfg.DefaultLanguage = b.Language
			break
		}
	}
	for _, b := range s.ImageBindings {
		if b.Domain == host {
			cfg.ImageBasePath = b.ImageBasePath
			break
		}
	}
	return cfg
}

func (s *Snapshot) ActiveApps() []App {
	out := make([]App, 0, len(s.Apps))
	for _, a := range s.Apps {
		if a.Active {
			out = append(out, a)
		}
	}
	return out
}

func (s *Snapshot) FindApp(slug string) (App, bool) {
	for _, a := range s.Apps {
		if a.Active && a.Slug == slug {
			return a, true
		}
	}
	return App{}, false
}

func (s *Snapshot) LinksFor(siteKey, kind, appSlug, language, host string) []SiteLink {
	out := []SiteLink{}
	host = snapshotHost(host)
	for _, l := range s.Links {
		if l.Enabled && l.SiteKey == siteKey && l.Kind == kind && l.Language == language && (l.AppSlug == "" || l.AppSlug == appSlug) && (l.Domain == "" || l.Domain == "global" || l.Domain == host) {
			out = append(out, l)
		}
	}
	return out
}

type runtimeBankJSON struct {
	Words     []string `json:"words"`
	Sentences []string `json:"sentences"`
}

func decodeRuntimeBank(payload string) (languageData, error) {
	var b runtimeBankJSON
	if err := json.Unmarshal([]byte(payload), &b); err != nil {
		return languageData{}, err
	}
	if len(b.Words) == 0 || len(b.Sentences) == 0 {
		return languageData{}, fmt.Errorf("bank requires words and sentences")
	}
	return languageData{words: b.Words, sentences: b.Sentences}, nil
}

// SortedInternalDomains returns matching enabled cross-domain navigation.
func (s *Snapshot) SortedInternalDomains(siteKey, lang string) []InternalDomain {
	out := []InternalDomain{}
	for _, d := range s.InternalDomains {
		if d.Enabled && d.SiteKey == siteKey && d.Language == lang {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight == out[j].Weight {
			return out[i].ID < out[j].ID
		}
		return out[i].Weight > out[j].Weight
	})
	return out
}
