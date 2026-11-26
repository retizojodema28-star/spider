package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bingo-project/component-base/log"
	"github.com/gin-gonic/gin"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"apk-html-go/internal/site"
)

func main() {
	options := log.NewDefaultOptions()
	options.Format = "json"
	options.Path = "/tmp/apk-html-go.log"
	options.MaxSize = 5
	log.Init(options)
	if err := run(); err != nil {
		log.Errorf("site stopped: %v", err)
		log.Sync()
		os.Exit(1)
	}
	log.Sync()
}

func run() error {
	adminOptions, err := site.AdminOptionsFromEnv()
	if err != nil {
		return err
	}
	db, err := openDatabase(os.Getenv("DATABASE_DSN"))
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	initCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := site.Migrate(db.WithContext(initCtx)); err != nil {
		return fmt.Errorf("migrate site: %w", err)
	}
	if err := site.MigrateAdmin(db.WithContext(initCtx)); err != nil {
		return fmt.Errorf("migrate admin: %w", err)
	}
	if err := site.Seed(db.WithContext(initCtx), env("SEED_FILE", "data/seed/apps.json")); err != nil {
		return err
	}
	if err := site.BootstrapAdminFromEnv(initCtx, db); err != nil {
		return err
	}
	runtime, err := site.NewRuntime(initCtx, db, site.RuntimeOptions{
		LanguageRoot: env("LANGUAGE_DATA_DIR", "data/lan"),
		AssetRoot:    env("ASSET_DIR", "assets"),
		Languages:    strings.Split(env("LANGUAGES", "id"), ","),
		OnError:      func(err error) { log.Errorf("runtime refresh failed (previous snapshot retained): %v", err) },
	})
	if err != nil {
		return err
	}
	if env("GIN_MODE", "release") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	recommendationFeed, err := site.NewRecommendationFeed(site.RecommendationFeedURL, nil)
	if err != nil {
		return fmt.Errorf("configure recommendation feed: %w", err)
	}
	router := site.NewRouterWithRuntime(runtime, site.RouterOptions{AssetRoot: env("ASSET_DIR", "assets"), Admin: &adminOptions, RecommendationFeed: recommendationFeed})
	server := &http.Server{
		Addr: ":" + env("PORT", "8080"), Handler: router,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20, ErrorLog: log.StdErrLogger(),
	}
	runtimeDone := make(chan struct{})
	go func() { defer close(runtimeDone); _ = runtime.Run(ctx) }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	log.Infof("site ready on %s; runtime revision %d", server.Addr, runtime.Snapshot().Revision)
	select {
	case err := <-serveErr:
		stop()
		<-runtimeDone
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			_ = server.Close()
		}
		<-runtimeDone
		return err
	}
}

func openDatabase(dsn string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DATABASE_DSN is required")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_DSN is invalid")
	}
	if cfg.DBName == "" || cfg.User == "" || cfg.Passwd == "" {
		return nil, fmt.Errorf("DATABASE_DSN must include database and credentials")
	}
	if mode := env("APP_ENV", "production"); mode != "development" && mode != "test" {
		password := strings.ToLower(cfg.Passwd)
		if len(cfg.Passwd) < 16 || strings.Contains(password, "password") || strings.Contains(password, "changeme") || strings.Contains(password, "example") {
			return nil, fmt.Errorf("production database password must be strong and non-default")
		}
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn), TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	conn, err := db.DB()
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(12)
	conn.SetMaxIdleConns(4)
	conn.SetConnMaxLifetime(5 * time.Minute)
	conn.SetConnMaxIdleTime(time.Minute)
	return db, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
