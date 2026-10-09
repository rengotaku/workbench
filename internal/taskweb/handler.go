package taskweb

import (
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/httpguard"
	"github.com/rengotaku/workbench/internal/taskapi"
)

// Handler serves the one page and mounts the API beside it.
type Handler struct {
	templates map[string]*template.Template
	staticFS  fs.FS
	api       *taskapi.API
	port      string
}

// NewHandler wires the page and the API together. api may be nil in tests that
// only exercise the page. port is the one the server listens on; requests for
// any other Host or Origin are refused.
func NewHandler(templates map[string]*template.Template, staticFS fs.FS, a *taskapi.API, port string) *Handler {
	return &Handler{templates: templates, staticFS: staticFS, api: a, port: port}
}

// Routes returns the router: the page, the static bundle, health, and /api.
func (h *Handler) Routes() http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), httpguard.New(h.port))

	r.StaticFS("/static", http.FS(h.staticFS))

	r.GET("/", h.handleIndex)
	r.GET("/add", h.handleAdd)
	r.GET("/healthz", h.handleHealthz)

	if h.api != nil {
		h.api.Mount(r)
	}
	return r
}

// handleIndex renders the shell. The list itself is fetched from the API on
// load, so the page and the CLI always read the same numbers.
func (h *Handler) handleIndex(c *gin.Context) {
	now := time.Now()
	h.render(c, "index.html", http.StatusOK, gin.H{
		"Title": "着手順",
		"Today": today(now),
	})
}

// handleAdd serves the page that is nothing but the add field, which a bare
// `todo` opens.
func (h *Handler) handleAdd(c *gin.Context) {
	h.render(c, "add.html", http.StatusOK, gin.H{
		"Title": "着手順に追加",
		"Today": today(time.Now()),
	})
}

func (h *Handler) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) render(c *gin.Context, name string, status int, data any) {
	t, ok := h.templates[name]
	if !ok {
		c.String(http.StatusInternalServerError, "Template error")
		return
	}
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, "base", data); err != nil {
		c.String(http.StatusInternalServerError, "Template error")
	}
}

func today(now time.Time) string {
	return now.Format("1/2") + " (" + weekdayJA(now.Weekday()) + ")"
}

func weekdayJA(d time.Weekday) string {
	return [...]string{"日", "月", "火", "水", "木", "金", "土"}[int(d)]
}
