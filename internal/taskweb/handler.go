package taskweb

import (
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/taskapi"
)

// Handler serves the one page and mounts the API beside it.
type Handler struct {
	templates map[string]*template.Template
	staticFS  fs.FS
	api       *taskapi.API
}

// NewHandler wires the page and the API together. api may be nil in tests that
// only exercise the page.
func NewHandler(templates map[string]*template.Template, staticFS fs.FS, a *taskapi.API) *Handler {
	return &Handler{templates: templates, staticFS: staticFS, api: a}
}

// Mount registers the pages, the static bundle and /api on r. The engine, its
// middleware and /healthz belong to the caller, so that the todo and note
// screens can share one server.
func (h *Handler) Mount(r gin.IRouter) {
	r.StaticFS("/static", http.FS(h.staticFS))

	r.GET("/", h.handleIndex)
	r.GET("/add", h.handleAdd)

	if h.api != nil {
		h.api.Mount(r)
	}
}

// handleIndex renders the shell. The list itself is fetched from the API on
// load, so the page and the CLI always read the same numbers.
func (h *Handler) handleIndex(c *gin.Context) {
	now := time.Now()
	h.render(c, "index.html", http.StatusOK, gin.H{
		"Title":   "着手順",
		"Current": "todo",
		"Today":   today(now),
	})
}

// handleAdd serves the page that is nothing but the add field, which a bare
// `todo` opens.
func (h *Handler) handleAdd(c *gin.Context) {
	h.render(c, "add.html", http.StatusOK, gin.H{
		"Title":   "着手順に追加",
		"Current": "todo",
		"Today":   today(time.Now()),
	})
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
