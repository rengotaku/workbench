package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// The band at the top is served as its own list, in queue order, so the page
// does not have to work out what is in it.
func TestFocus_AppearsInItsOwnListAndStaysInTheQueue(t *testing.T) {
	store, h := setup(t)
	_, _ = store.Add("A", "", "", task.ActorHuman)
	b, _ := store.Add("B", "", "", task.ActorHuman)
	_, _ = store.Add("C", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+b.ID+"/focus",
		map[string]any{"on": true})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, taskOf(t, body)["focused"])

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	assert.Len(t, rowsOf(t, list, "queue"), 3, "it is still in the queue")

	focus := rowsOf(t, list, "focus")
	require.Len(t, focus, 1)
	row, ok := focus[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, b.ID, row["id"])
}

func TestFocus_NeedsTheFlag(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("A", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/focus", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "on")
}

func TestFocus_ClearedByCompleting(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("A", "", "", task.ActorHuman)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/focus", map[string]any{"on": true})
	require.Equal(t, http.StatusOK, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/done", nil)
	require.Equal(t, http.StatusOK, code)

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	assert.Empty(t, rowsOf(t, list, "focus"))
}
