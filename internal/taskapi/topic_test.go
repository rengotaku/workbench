package taskapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func topicsOf(t *testing.T, h http.Handler) []any {
	t.Helper()
	code, body := call(t, h, http.MethodGet, "/api/tasks?scope=all", nil)
	require.Equal(t, http.StatusOK, code)
	topics, ok := body["topics"].([]any)
	require.True(t, ok, "topics is always an array")
	return topics
}

func TestTopic_PatchSetsClearsAndShowsInList(t *testing.T) {
	store, h := setup(t)
	a, err := store.Add("a", "", "", task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+a.ID, map[string]any{"topic": " 青環離 "})
	require.Equal(t, http.StatusOK, code)
	tk, ok := body["task"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "青環離", tk["topic"])

	_, list := call(t, h, http.MethodGet, "/api/tasks?scope=all", nil)
	queue, ok := list["queue"].([]any)
	require.True(t, ok)
	row, ok := queue[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "青環離", row["topic"])

	code, body = call(t, h, http.MethodPatch, "/api/tasks/"+a.ID, map[string]any{"topic": ""})
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body["task"], "topic")
}

func TestTopic_TooLongOrMultilineIs400AndChangesNothing(t *testing.T) {
	store, h := setup(t)
	a, err := store.Add("a", "", "", task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+a.ID, map[string]any{
		"name": "renamed", "topic": strings.Repeat("あ", task.MaxTopicLen+1),
	})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.NotEmpty(t, body["error"])
	got, _ := store.Get(a.ID)
	assert.Equal(t, "a", got.Name, "the whole request is refused")

	code, _ = call(t, h, http.MethodPatch, "/api/tasks/"+a.ID, map[string]any{"topic": "a\nb"})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestTopic_UnknownTaskIs404(t *testing.T) {
	_, h := setup(t)
	code, _ := call(t, h, http.MethodPatch, "/api/tasks/T999", map[string]any{"topic": "x"})
	assert.Equal(t, http.StatusNotFound, code)
}

func TestTopic_UndoThroughAPI(t *testing.T) {
	store, h := setup(t)
	a, err := store.Add("a", "", "", task.ActorHuman)
	require.NoError(t, err)
	call(t, h, http.MethodPatch, "/api/tasks/"+a.ID, map[string]any{"topic": "x"})
	code, _ := call(t, h, http.MethodPost, "/api/undo", nil)
	require.Equal(t, http.StatusOK, code)
	got, _ := store.Get(a.ID)
	assert.Empty(t, got.Topic)
}

func TestTopic_ListGivesEachTopicOnceInQueueOrder(t *testing.T) {
	store, h := setup(t)
	assert.Empty(t, topicsOf(t, h), "no topics is [] not null")

	ids := make([]string, 0, 4)
	for _, n := range []string{"a", "b", "c", "d"} {
		x, err := store.Add(n, "", "", task.ActorHuman)
		require.NoError(t, err)
		ids = append(ids, x.ID)
	}
	for i, tp := range []string{"B", "A", "", "B"} {
		_, err := store.SetTopic(ids[i], tp, task.ActorHuman)
		require.NoError(t, err)
	}
	assert.Equal(t, []any{"B", "A"}, topicsOf(t, h))

	// A topic used only by a waiting task is still offered, after the queue's.
	w, err := store.Add("w", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.SetTopic(w.ID, "W", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Wait(w.ID, "誰か", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []any{"B", "A", "W"}, topicsOf(t, h))
}
