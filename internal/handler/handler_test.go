package handler_test

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/rengotaku/todo-order/internal/handler"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func makeTestTemplates() map[string]*template.Template {
	parse := func(src string) *template.Template {
		return template.Must(template.New("base").Parse(`{{define "base"}}` + src + `{{end}}`))
	}
	return map[string]*template.Template{
		"index.html": parse(`<h1>着手順</h1><span>{{.Today}}</span><div data-base="{{.TicketURLBase}}"></div>`),
	}
}

func setupHandler() *handler.Handler {
	staticFS := fstest.MapFS{
		"css/style.css": &fstest.MapFile{Data: []byte("body{color:#000}")},
	}
	return handler.NewHandler(makeTestTemplates(), fs.FS(staticFS), nil, "")
}

func TestHandler_Index(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "着手順")
}

func TestHandler_Healthz(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHandler_Static(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "body{color:#000}")
}

// The ticket URL base reaches the page so a key in a task name can become a
// link. With no base configured the attribute is empty and names stay plain.
func TestHandler_TicketURLBaseReachesThePage(t *testing.T) {
	staticFS := fstest.MapFS{"css/style.css": &fstest.MapFile{Data: []byte("")}}
	h := handler.NewHandler(makeTestTemplates(), fs.FS(staticFS), nil, "https://example.test/view/")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-base="https://example.test/view/"`)
}

func TestHandler_NoTicketURLBaseLeavesTheAttributeEmpty(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Contains(t, rec.Body.String(), `data-base=""`)
}

// The API is optional so the page can be tested on its own; when it is absent
// the route must simply not exist rather than panic.
func TestHandler_NoAPIMounted(t *testing.T) {
	h := setupHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
