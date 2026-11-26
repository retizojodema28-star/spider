package main

import (
	"context"
	"errors"
	"github.com/go-sql-driver/mysql"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"apk-html-go/internal/site"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		return errors.New("DATABASE_DSN is required")
	}
	driverConfig, err := mysql.ParseDSN(dsn)
	if err != nil {
		return errors.New("invalid DATABASE_DSN")
	}
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	if driverConfig.Timeout == 0 {
		driverConfig.Timeout = 5 * time.Second
	}
	if driverConfig.ReadTimeout == 0 {
		driverConfig.ReadTimeout = 10 * time.Second
	}
	if driverConfig.WriteTimeout == 0 {
		driverConfig.WriteTimeout = 10 * time.Second
	}
	db, err := gorm.Open(gormmysql.Open(driverConfig.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("worker database connection failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("worker database unavailable")
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := site.MigrateAdmin(db.WithContext(initCtx)); err != nil {
		return errors.New("worker schema migration failed")
	}
	root := os.Getenv("LANGUAGE_DATA_DIR")
	if root == "" {
		root = "data/lan"
	}
	langs := []string{"id"}
	if raw := os.Getenv("LANGUAGES"); raw != "" {
		langs = strings.Split(raw, ",")
	}
	w, err := site.NewWorker(db, site.WorkerOptions{LanguageRoot: root, Languages: langs, OnError: func(err error) { log.Printf("worker: %v", err) }})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			rw.WriteHeader(405)
			return
		}
		check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		rw.Header().Set("Content-Type", "application/json")
		if w.Health(check) != nil {
			rw.WriteHeader(503)
			_, _ = rw.Write([]byte(`{"ok":false}`))
			return
		}
		_, _ = rw.Write([]byte(`{"ok":true}`))
	})
	port := os.Getenv("WORKER_HEALTH_PORT")
	if port == "" {
		port = "8081"
	}
	addr := net.JoinHostPort("", port)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	errorsCh := make(chan error, 1)
	workerDone := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	go func() { workerDone <- w.Run(ctx) }()
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
		}
	}
	stop()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
	// Wait for the worker to record cancellation before closing the DB.
	select {
	case <-workerDone:
	case <-shutdown.Done():
		return errors.New("worker shutdown timed out")
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
