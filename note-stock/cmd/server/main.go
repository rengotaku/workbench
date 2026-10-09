package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lmittmann/tint"
	"github.com/sethvargo/go-envconfig"

	"github.com/rengotaku/note-stock/internal/note"
	"github.com/rengotaku/note-stock/internal/pages"
	"github.com/rengotaku/note-stock/web"
)

type Config struct {
	// Listen is loopback-only by default: the memos are not served to the
	// network, and there is no authentication in front of them.
	Listen          string        `env:"LISTEN_ADDR,default=127.0.0.1"`
	Port            string        `env:"PORT,default=8086"`
	AppEnv          string        `env:"APP_ENV"`
	DataDir         string        `env:"DATA_DIR"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,default=10s"`
}

// defaultDataDir keeps notes.json and the event logs under the user's
// application data, outside the repository.
func defaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "note-stock"), nil
}

func main() {
	var logLevel slog.LevelVar
	if l := os.Getenv("LOG_LEVEL"); l != "" {
		_ = logLevel.UnmarshalText([]byte(l))
	}

	var logHandler slog.Handler
	if os.Getenv("APP_ENV") == "production" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &logLevel})
	} else {
		logHandler = tint.NewHandler(os.Stderr, &tint.Options{Level: &logLevel, TimeFormat: time.Kitchen})
	}
	slog.SetDefault(slog.New(logHandler))

	var cfg Config
	if err := envconfig.Process(context.Background(), &cfg); err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	templates, err := pages.LoadTemplates(web.FS)
	if err != nil {
		slog.Error("failed to load templates", "error", err)
		os.Exit(1)
	}
	staticFiles, err := fs.Sub(web.FS, "static")
	if err != nil {
		slog.Error("failed to load static files", "error", err)
		os.Exit(1)
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		if dataDir, err = defaultDataDir(); err != nil {
			slog.Error("failed to resolve data dir", "error", err)
			os.Exit(1)
		}
	}
	store, err := note.Open(dataDir, nil)
	if err != nil {
		slog.Error("failed to open note store", "error", err, "dir", dataDir)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()
	slog.Info("note store ready", "dir", dataDir)

	srv := &http.Server{
		Addr:         cfg.Listen + ":" + cfg.Port,
		Handler:      pages.New(templates, staticFiles, store, cfg.Port).Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
