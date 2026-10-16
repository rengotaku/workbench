package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func linksOf(t *testing.T, body map[string]any) []any {
	t.Helper()
	tk, ok := body["task"].(map[string]any)
	require.True(t, ok, "response has a task")
	l, _ := tk["links"].([]any)
	return l
}

func TestAddTask_WithLinksNumbersThemAndOneUndoRemovesTheTask(t *testing.T) {
	store, h := setup(t)

	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{
		"name": "調べる [1] [2]",
		"links": []map[string]string{
			{"url": "https://a.example/", "label": "仕様"},
			{"url": "https://b.example/"},
		},
	})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []any{
		map[string]any{"n": float64(1), "url": "https://a.example/", "label": "仕様"},
		map[string]any{"n": float64(2), "url": "https://b.example/"},
	}, linksOf(t, body))
	id, _ := body["task"].(map[string]any)["id"].(string)

	code, _ = call(t, h, http.MethodPost, "/api/undo", nil)
	require.Equal(t, http.StatusOK, code)
	_, err := store.Get(id)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestAddTask_WithoutLinksHasNoLinksKey(t *testing.T) {
	_, h := setup(t)
	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "x"})
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body["task"], "links")
}

func TestAddTask_InvalidLinkIs400(t *testing.T) {
	store, h := setup(t)
	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{
		"name": "x", "links": []map[string]string{{"url": "ftp://a.example/"}},
	})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "関連 URL [1]")
	list, _ := store.List()
	assert.Empty(t, list)
}

func TestPatchTask_LinksReplaceClearAndAbsentLeaveAlone(t *testing.T) {
	store, h := setup(t)
	added, err := store.AddWithLinks("x", "", "", []task.LinkInput{{URL: "https://a.example/"}}, task.ActorHuman)
	require.NoError(t, err)
	path := "/api/tasks/" + added.ID

	code, body := call(t, h, http.MethodPatch, path, map[string]any{"detail": "d"})
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, linksOf(t, body), 1, "links absent: untouched")

	code, body = call(t, h, http.MethodPatch, path, map[string]any{
		"links": []map[string]string{{"url": "https://b.example/", "label": "b"}, {"url": "https://a.example/"}},
	})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []any{
		map[string]any{"n": float64(1), "url": "https://b.example/", "label": "b"},
		map[string]any{"n": float64(2), "url": "https://a.example/"},
	}, linksOf(t, body))

	evs, _ := store.Events(added.ID)
	n := len(evs)
	code, _ = call(t, h, http.MethodPatch, path, map[string]any{
		"links": []map[string]string{{"url": "https://b.example/", "label": "b"}, {"url": "https://a.example/"}},
	})
	require.Equal(t, http.StatusOK, code)
	evs, _ = store.Events(added.ID)
	assert.Len(t, evs, n, "same list: nothing recorded")

	code, body = call(t, h, http.MethodPatch, path, map[string]any{"links": []map[string]string{}})
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, linksOf(t, body))

	code, _ = call(t, h, http.MethodPost, "/api/undo", nil)
	require.Equal(t, http.StatusOK, code)
	got, _ := store.Get(added.ID)
	assert.Len(t, got.Links, 2, "undo of a clear restores the list")
}

func TestPatchTask_InvalidLinksIs400(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("x", "", "", task.ActorHuman)
	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+added.ID, map[string]any{
		"links": []map[string]string{{"url": ""}},
	})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "http(s)")
}
