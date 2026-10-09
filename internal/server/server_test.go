package server_test

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/notepages"
	"github.com/rengotaku/workbench/internal/server"
	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
	"github.com/rengotaku/workbench/internal/taskweb"
	noteweb "github.com/rengotaku/workbench/web/note"
	sharedweb "github.com/rengotaku/workbench/web/shared"
	todoweb "github.com/rengotaku/workbench/web/todo"
)

const port = "8085"

func newServer(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)

	taskStore, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = taskStore.Close() })
	noteStore, err := note.Open(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = noteStore.Close() })

	todoTemplates, err := todoTemplates()
	require.NoError(t, err)
	todoStatic, err := fs.Sub(todoweb.FS, "static")
	require.NoError(t, err)
	noteTemplates, err := notepages.LoadTemplates(noteweb.FS)
	require.NoError(t, err)
	noteStatic, err := fs.Sub(noteweb.FS, "static")
	require.NoError(t, err)

	return server.New(port,
		taskweb.NewHandler(todoTemplates, todoStatic, taskapi.New(taskStore, time.Local)),
		notepages.New(noteTemplates, noteStatic, noteStore),
	)
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:" + port
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// gin.New panics on a duplicate route, so building the server is the check
// that todo and note do not collide; the screens must also answer.
func TestScreensAndHealth(t *testing.T) {
	h := newServer(t)
	for path, want := range map[string]string{
		"/":               "着手順",
		"/add":            "着手順に追加",
		"/notes":          `id="memo"`,
		"/notes/timeline": "note タイムライン",
		"/healthz":        `"status":"ok"`,
	} {
		rec := do(h, http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Contains(t, rec.Body.String(), want, path)
	}
	// The note screens moved: the old paths belong to nobody.
	assert.Equal(t, http.StatusNotFound, do(h, http.MethodGet, "/timeline", "").Code)
}

var assetRef = regexp.MustCompile(`(?:src|href)="(/[^"]*static/[^"]+)"`)

func TestStaticAssetsReferencedByPagesResolve(t *testing.T) {
	h := newServer(t)
	for _, page := range []string{"/", "/add", "/notes", "/notes/timeline"} {
		refs := assetRef.FindAllStringSubmatch(do(h, http.MethodGet, page, "").Body.String(), -1)
		assert.NotEmpty(t, refs, page)
		for _, m := range refs {
			rec := do(h, http.MethodGet, m[1], "")
			assert.Equal(t, http.StatusOK, rec.Code, "%s -> %s", page, m[1])
			assert.NotEmpty(t, rec.Body.String(), m[1])
		}
	}
}

func TestBothAPIsAreMounted(t *testing.T) {
	h := newServer(t)
	assert.Equal(t, http.StatusOK, do(h, http.MethodGet, "/api/tasks", "").Code)
	assert.Equal(t, http.StatusOK, do(h, http.MethodGet, "/api/topics", "").Code)
	assert.Equal(t, http.StatusCreated, do(h, http.MethodPost, "/api/notes", `{"text":"x"}`).Code)
	assert.Equal(t, http.StatusOK, do(h, http.MethodGet, "/api/notes", "").Code)
}

func TestGuardCoversBothHalves(t *testing.T) {
	h := newServer(t)
	for _, path := range []string{"/", "/notes", "/api/tasks", "/api/notes"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "evil.example:" + port
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, path)
	}
}

func todoTemplates() (map[string]*template.Template, error) {
	m := map[string]*template.Template{}
	for _, name := range []string{"index.html", "add.html"} {
		t, err := sharedweb.Parse(todoweb.FS, name)
		if err != nil {
			return nil, err
		}
		m[name] = t
	}
	return m, nil
}

// Every screen carries the same header: links to todo, note and the timeline,
// with aria-current on the one the reader is on.
func TestSiteHeaderOnEveryScreen(t *testing.T) {
	h := newServer(t)
	links := []string{`href="/"`, `href="/notes"`, `href="/notes/timeline"`}
	screens := []struct{ path, current string }{
		{"/", `<a href="/" aria-current="page">`},
		{"/add", `<a href="/" aria-current="page">`},
		{"/notes", `<a href="/notes" aria-current="page">`},
		{"/notes/timeline", `<a href="/notes/timeline" aria-current="page">`},
	}
	for _, s := range screens {
		rec := do(h, http.MethodGet, s.path, "")
		require.Equal(t, http.StatusOK, rec.Code, s.path)
		body := rec.Body.String()
		start := strings.Index(body, `class="site-nav"`)
		require.GreaterOrEqual(t, start, 0, s.path)
		nav := body[start : start+strings.Index(body[start:], "</nav>")]
		for _, l := range links {
			assert.Contains(t, nav, l, s.path)
		}
		assert.Contains(t, nav, s.current, s.path)
		assert.Equal(t, 1, strings.Count(nav, `aria-current="page"`), s.path)
		assert.Contains(t, body, "/shared/css/tokens.css", s.path)
		assert.Contains(t, body, "/shared/js/common.js", s.path)
	}
}

func TestSharedStaticServedBehindGuard(t *testing.T) {
	h := newServer(t)
	for _, p := range []string{"/shared/css/tokens.css", "/shared/css/base.css", "/shared/js/common.js"} {
		assert.Equal(t, http.StatusOK, do(h, http.MethodGet, p, "").Code, p)

		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Host = "evil.example:" + port
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, p)
	}
}
