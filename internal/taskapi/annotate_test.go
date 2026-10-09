package taskapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func TestAnnotateTask_AppendsWithTheRequestActorAsAuthor(t *testing.T) {
	store, h := setup(t)
	added, err := store.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)

	code, out := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/annotations",
		map[string]any{"kind": "quote", "body": "引用", "source": "slack", "actor": "ai"})
	require.Equal(t, http.StatusCreated, code, out)
	an, ok := out["annotation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ai", an["author"])
	assert.Equal(t, "slack", an["source"])

	// The list carries the annotations so the page can show them.
	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	queue, ok := list["queue"].([]any)
	require.True(t, ok)
	first, ok := queue[0].(map[string]any)
	require.True(t, ok)
	assert.Len(t, first["annotations"], 1)
}

func TestAnnotateTask_Errors(t *testing.T) {
	store, h := setup(t)
	added, err := store.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/annotations", map[string]any{"kind": "nope", "body": "b"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/annotations", map[string]any{"kind": "memo", "body": ""})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/annotations", map[string]any{"kind": "memo", "body": strings.Repeat("a", task.MaxAnnotationBody+1)})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks/T999/annotations", map[string]any{"kind": "memo", "body": "b"})
	assert.Equal(t, http.StatusNotFound, code)
}
