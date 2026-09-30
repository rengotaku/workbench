package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/todo-order/internal/task"
)

// A task added with a pasted link is one change: a single undo takes the whole
// task back out, not just its link.
func TestAddTask_WithLinkIsUndoneInOneStep(t *testing.T) {
	store, h := setup(t)

	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{
		"name": "DSC-1 チケット", "url": "https://example.com/DSC-1", "link_text": "DSC-1",
	})
	require.Equal(t, http.StatusOK, code)
	added, ok := body["task"].(map[string]any)
	require.True(t, ok, "response has a task")
	id, ok := added["id"].(string)
	require.True(t, ok, "task has an id")
	assert.Equal(t, "https://example.com/DSC-1", added["url"])
	assert.Equal(t, "DSC-1", added["link_text"])

	code, _ = call(t, h, http.MethodPost, "/api/undo", nil)
	require.Equal(t, http.StatusOK, code)

	_, err := store.Get(id)
	assert.ErrorIs(t, err, task.ErrNotFound)
}
