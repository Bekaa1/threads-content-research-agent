package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/leadwatch"
	"github.com/Egor01KKK/threads-content-research-agent/threads"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := leadwatch.ConfigFromEnv()
	if err != nil {
		logger.Error("invalid worker configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		logger.Error("could not create database client")
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	store, err := leadwatch.NewPostgresStore(ctx, db)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}

	threadConfig := threads.DefaultConfig()
	threadConfig.Session = "" // Keep collection anonymous; never pass login cookies to the scraper.
	threadConfig.CSRF = ""
	threadConfig.Delay = time.Second
	threadConfig.NoCache = true
	threadConfig.Retries = 2
	threadConfig.Timeout = 25 * time.Second
	searcher, err := threads.NewClient(threadConfig)
	if err != nil {
		logger.Error("could not initialize public Threads search client", "error", err)
		os.Exit(1)
	}
	analyzer, err := leadwatch.NewGroqAnalyzer(cfg.GroqAPIKey, cfg.GroqModel, nil)
	if err != nil {
		logger.Error("could not initialize classifier", "error", err)
		os.Exit(1)
	}
	notifier, err := leadwatch.NewTelegramNotifier(cfg.TelegramBot, cfg.TelegramChat, nil)
	if err != nil {
		logger.Error("could not initialize Telegram notifier", "error", err)
		os.Exit(1)
	}
	worker, err := leadwatch.NewWorker(searcher, analyzer, notifier, store, cfg.Queries, cfg.OfferProfile, logger)
	if err != nil {
		logger.Error("could not initialize lead worker", "error", err)
		os.Exit(1)
	}
	if cfg.CollectionMode == "ingest" {
		worker.UseIngestOnly()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /admin/posts", recentPostsHandler(cfg.ReadAPIKey, store))
	if cfg.CollectionMode == "ingest" {
		mux.Handle("POST /ingest/posts", ingestPostsHandler(cfg.IngestAPIKey, store, worker.Trigger, logger))
	}
	server := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()
	logger.Info("lead worker started", "interval_minutes", int(cfg.Interval.Minutes()), "queries", len(cfg.Queries), "port", cfg.Port, "collection_mode", cfg.CollectionMode)

	workerErr := make(chan error, 1)
	go func() { workerErr <- worker.Run(ctx, cfg.Interval) }()
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		logger.Error("health server stopped", "error", err)
	case err := <-workerErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("lead worker stopped", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
