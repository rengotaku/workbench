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

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/notepages"
	"github.com/rengotaku/workbench/internal/noteroute"
	"github.com/rengotaku/workbench/internal/server"
	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
	"github.com/rengotaku/workbench/internal/taskweb"
	noteweb "github.com/rengotaku/workbench/web/note"
	sharedweb "github.com/rengotaku/workbench/web/shared"
	todoweb "github.com/rengotaku/workbench/web/todo"
)

type Config struct {
	// Listen is loopback-only by default: the list and the memos are not
	// served to the network, and there is no authentication in front of them.
	Listen          string        `env:"LISTEN_ADDR,default=127.0.0.1"`
	Port            string        `env:"PORT,default=8085"`
	AppEnv          string        `env:"APP_ENV"`
	TodoDataDir     string        `env:"TODO_DATA_DIR"`
	NoteDataDir     string        `env:"NOTE_DATA_DIR"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,default=10s"`
}

// defaultDataDir keeps the data under the user's data directory, outside the
// repository. name is the directory the standalone servers already used.
func defaultDataDir(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", name), nil
}

func main() {
	os.Exit(run())
}

// run returns the exit code so that deferred store Close calls run before the
// process exits.
func run() int {
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
		return 1
	}
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	todoTemplates, err := loadTodoTemplates()
	if err != nil {
		slog.Error("failed to load todo templates", "error", err)
		return 1
	}
	todoStatic, err := fs.Sub(todoweb.FS, "static")
	if err != nil {
		slog.Error("failed to load todo static files", "error", err)
		return 1
	}
	noteTemplates, err := notepages.LoadTemplates(noteweb.FS)
	if err != nil {
		slog.Error("failed to load note templates", "error", err)
		return 1
	}
	noteStatic, err := fs.Sub(noteweb.FS, "static")
	if err != nil {
		slog.Error("failed to load note static files", "error", err)
		return 1
	}

	todoDir := cfg.TodoDataDir
	if todoDir == "" {
		if todoDir, err = defaultDataDir("todo-order"); err != nil {
			slog.Error("failed to resolve todo data dir", "error", err)
			return 1
		}
	}
	noteDir := cfg.NoteDataDir
	if noteDir == "" {
		if noteDir, err = defaultDataDir("note-stock"); err != nil {
			slog.Error("failed to resolve note data dir", "error", err)
			return 1
		}
	}

	taskStore, err := task.Open(todoDir, time.Local)
	if err != nil {
		slog.Error("failed to open task store", "error", err, "dir", todoDir)
		return 1
	}
	defer func() { _ = taskStore.Close() }()
	slog.Info("task store ready", "dir", todoDir)

	noteStore, err := note.Open(noteDir, nil)
	if err != nil {
		slog.Error("failed to open note store", "error", err, "dir", noteDir)
		return 1
	}
	defer func() { _ = noteStore.Close() }()
	slog.Info("note store ready", "dir", noteDir)

	handler := server.New(cfg.Port,
		taskweb.NewHandler(todoTemplates, todoStatic, taskapi.New(taskStore, time.Local)),
		notepages.New(noteTemplates, noteStatic, noteStore),
		noteroute.New(noteStore, taskStore, time.Local),
	)

	srv := &http.Server{
		Addr:         cfg.Listen + ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		slog.Error("server error", "error", err)
		return 1
	case <-quit:
	}

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		// Handlers may still be writing. Exit without the deferred Close so the
		// data directory locks are held until the process is gone; releasing
		// them early would let another server open a store mid-write.
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
	return 0
}

// loadTodoTemplates builds a per-page template set so each page gets its own
// "content" definition. A single shared set causes {{define "content"}} to be
// overwritten by the last-parsed file, breaking all but the final page.
func loadTodoTemplates() (map[string]*template.Template, error) {
	pages := []struct{ name, path string }{
		{"index.html", "templates/index.html"},
		{"add.html", "templates/add.html"},
	}

	m := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		t, err := sharedweb.Parse(todoweb.FS, p.name)
		if err != nil {
			return nil, fmt.Errorf("loading template %s: %w", p.name, err)
		}
		m[p.name] = t
	}
	return m, nil
}
