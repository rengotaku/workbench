package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
)

// The add succeeds and the topic PATCH fails: the error must still name the
// added task, so it can be fixed with todo topic instead of adding it again.
func TestAdd_TopicFailureKeepsTheAddedID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if c.Request.Method == http.MethodPatch {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "boom"})
			return
		}
		c.Next()
	})
	taskapi.New(store, time.Local).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	err = failTodo(t, srv.URL, "資料", "--topic", "青環離")
	require.Error(t, err)
	tasks, _ := store.List()
	require.Len(t, tasks, 1)
	assert.Contains(t, err.Error(), "追加 "+tasks[0].ID+" は済んだ")
	assert.Contains(t, err.Error(), "todo topic "+tasks[0].ID+" 青環離")
}
