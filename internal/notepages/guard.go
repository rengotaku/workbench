package notepages

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// guard refuses requests that a web page on another origin could send to the
// loopback server: a foreign Host (DNS rebinding), a foreign Origin, and a
// write to /api that is not JSON (a cross-origin form or text/plain POST
// needs no preflight, so it would otherwise land).
func guard(port string) gin.HandlerFunc {
	hosts := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	origins := map[string]bool{"http://127.0.0.1:" + port: true, "http://localhost:" + port: true}

	return func(c *gin.Context) {
		if !hosts[c.Request.Host] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "この Host では受け付けません。http://127.0.0.1:" + port + " で開いてください"})
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && !origins[origin] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "別の Origin からの要求は受け付けません"})
			return
		}
		if needsJSON(c.Request) && !isJSON(c.GetHeader("Content-Type")) {
			c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type は application/json にしてください"})
			return
		}
		c.Next()
	}
}

// needsJSON is true for the API writes that carry a body. DELETE has none, so
// it is covered by the Host and Origin checks only.
func needsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") && (r.Method == http.MethodPost || r.Method == http.MethodPut)
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}
