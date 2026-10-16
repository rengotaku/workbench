package taskweb_test

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/httpguard"
	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
	"github.com/rengotaku/workbench/internal/taskweb"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func makeTestTemplates() map[string]*template.Template {
	parse := func(src string) *template.Template {
		return template.Must(template.New("base").Parse(`{{define "base"}}` + src + `{{end}}`))
	}
	return map[string]*template.Template{
		"index.html": parse(`<h1>着手順</h1><span>{{.View}}</span>`),
		"add.html":   parse(`<h1>{{.Title}}</h1><span>{{.View}}</span>`),
	}
}

const testPort = "8085"

// routes mounts h the way cmd/server does: behind the guard, on one engine.
func routes(h *taskweb.Handler) http.Handler {
	r := gin.New()
	r.Use(httpguard.New(testPort))
	h.Mount(r)
	return r
}

func setupHandler() *taskweb.Handler {
	staticFS := fstest.MapFS{
		"css/style.css": &fstest.MapFile{Data: []byte("body{color:#000}")},
	}
	return taskweb.NewHandler(makeTestTemplates(), fs.FS(staticFS), nil)
}

func TestHandler_Index(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:" + testPort
	rec := httptest.NewRecorder()
	routes(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "着手順")
}

func TestHandler_Add(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/add", nil)
	req.Host = "127.0.0.1:" + testPort
	rec := httptest.NewRecorder()
	routes(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "着手順に追加")
}

func TestHandler_Static(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	req.Host = "127.0.0.1:" + testPort
	rec := httptest.NewRecorder()
	routes(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "body{color:#000}")
}

// The API is optional so the page can be tested on its own; when it is absent
// the route must simply not exist rather than panic.
func TestHandler_NoAPIMounted(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Host = "127.0.0.1:" + testPort
	rec := httptest.NewRecorder()
	routes(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func send(h http.Handler, method, path, host string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The loopback guard fronts the API: a request that a page on another origin
// could send must be refused and must not change anything.
func TestHandler_Guard(t *testing.T) {
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	added, err := store.Add("a", "", "", task.ActorHuman)
	require.NoError(t, err)

	h := routes(taskweb.NewHandler(makeTestTemplates(), fs.FS(fstest.MapFS{}),
		taskapi.New(store, time.Local)))
	good := "127.0.0.1:" + testPort
	js := map[string]string{"Content-Type": "application/json"}

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		hdr    map[string]string
		body   string
		want   int
	}{
		{"loopback GET", "GET", "/api/tasks", good, nil, "", 200},
		{"foreign host (DNS rebinding)", "GET", "/api/tasks", "evil.example:" + testPort, nil, "", 403},
		{"wrong port", "GET", "/api/tasks", "127.0.0.1:9999", nil, "", 403},
		{"foreign origin GET", "GET", "/api/tasks", good, map[string]string{"Origin": "https://evil.example"}, "", 403},
		{"foreign origin POST", "POST", "/api/tasks/" + added.ID + "/done", good, map[string]string{"Origin": "https://evil.example", "Content-Type": "application/json"}, `{}`, 403},
		{"null origin", "POST", "/api/undo", good, map[string]string{"Origin": "null", "Content-Type": "application/json"}, `{}`, 403},
		{"body-less POST without JSON", "POST", "/api/tasks/" + added.ID + "/done", good, nil, "", 415},
		{"form POST", "POST", "/api/tasks", good, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "name=x", 415},
		{"text/plain POST", "POST", "/api/tasks", good, map[string]string{"Content-Type": "text/plain"}, `{"name":"x"}`, 415},
		{"text/plain PATCH", "PATCH", "/api/tasks/" + added.ID, good, map[string]string{"Content-Type": "text/plain"}, `{"name":"x"}`, 415},
		{"PATCH without content type", "PATCH", "/api/tasks/" + added.ID, good, nil, `{"name":"x"}`, 415},
		{"json PATCH", "PATCH", "/api/tasks/" + added.ID, good, js, `{"name":"renamed"}`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, send(h, tt.method, tt.path, tt.host, tt.hdr, tt.body).Code)
		})
	}

	// Nothing the refused requests asked for may have happened.
	tasks, _ := store.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, "renamed", tasks[0].Name)
	assert.Equal(t, task.StateOpen, tasks[0].State)
}
