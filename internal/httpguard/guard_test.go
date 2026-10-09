package httpguard_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/httpguard"
)

func TestNewRequiresJSONForEveryAPIWriteMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpguard.New("8000"))
	r.Any("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	do := func(method, path, ct string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(""))
		req.Host = "127.0.0.1:8000"
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, m := range []string{"POST", "PUT", "PATCH"} {
		if got := do(m, "/api/x", ""); got != http.StatusUnsupportedMediaType {
			t.Errorf("%s without content type = %d, want 415", m, got)
		}
		if got := do(m, "/api/x", "application/json"); got != http.StatusNoContent {
			t.Errorf("%s json = %d, want 204", m, got)
		}
	}
	if got := do("DELETE", "/api/x", ""); got != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", got)
	}
	if got := do("POST", "/other", ""); got != http.StatusNoContent {
		t.Errorf("POST outside /api = %d, want 204", got)
	}
}
