package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
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

	"github.com/rengotaku/todo-order/internal/api"
	"github.com/rengotaku/todo-order/internal/handler"
	"github.com/rengotaku/todo-order/internal/task"
	"github.com/rengotaku/todo-order/web"
)

type Config struct {
	// Listen is loopback-only by default: the list is not served to the
	// network, and there is no authentication in front of it.
	Listen          string        `env:"LISTEN_ADDR,default=127.0.0.1"`
	Port            string        `env:"PORT,default=8085"`
	AppEnv          string        `env:"APP_ENV"`
	DataDir         string        `env:"DATA_DIR"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,default=10s"`
}

// defaultDataDir keeps tasks.json and the event logs under the user's data
// directory, outside the repository.
func defaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "todo-order"), nil
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
		logHandler = tint.NewHandler(os.Stderr, &tint.Options{
			Level:      &logLevel,
			TimeFormat: time.Kitchen,
		})
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

	templates, err := loadTemplates()
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

	store, err := task.Open(dataDir, time.Local)
	if err != nil {
		slog.Error("failed to open task store", "error", err, "dir", dataDir)
		os.Exit(1)
	}
	slog.Info("task store ready", "dir", dataDir)

	h := handler.NewHandler(templates, staticFiles, api.New(store, time.Local))

	srv := &http.Server{
		Addr:         cfg.Listen + ":" + cfg.Port,
		Handler:      h.Routes(),
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

// loadTemplates builds a per-page template set so each page gets its own
// "content" definition. A single shared set causes {{define "content"}} to be
// overwritten by the last-parsed file, breaking all but the final page.
func loadTemplates() (map[string]*template.Template, error) {
	pages := []struct{ name, path string }{
		{"index.html", "templates/index.html"},
	}

	m := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		t, err := template.ParseFS(web.FS, "templates/base.html", p.path)
		if err != nil {
			return nil, fmt.Errorf("loading template %s: %w", p.name, err)
		}
		m[p.name] = t
	}
	return m, nil
}
