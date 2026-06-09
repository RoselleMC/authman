package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RoselleMC/authman/core/internal/config"
	"github.com/RoselleMC/authman/core/internal/server"
	"github.com/RoselleMC/authman/core/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	var playerStore store.PlayerStore
	var postgresStore *store.Postgres
	if cfg.DatabaseURL != "" {
		dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		postgresStore, err = store.OpenPostgres(dbCtx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("failed to connect postgres", "error", err)
			os.Exit(1)
		}
		defer postgresStore.Close()
		if err := postgresStore.Migrate(dbCtx); err != nil {
			logger.Error("failed to migrate postgres", "error", err)
			os.Exit(1)
		}
		playerStore = postgresStore
	}

	options := server.Options{
		Config: cfg,
		Logger: logger,
		Store:  playerStore,
	}
	if postgresStore != nil {
		options.Nodes = postgresStore
	}
	app := server.New(options)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("authman http server starting", "addr", cfg.HTTPAddr)
		errc <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown failed", "error", err)
			os.Exit(1)
		}
		logger.Info("authman http server stopped")
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}
}
