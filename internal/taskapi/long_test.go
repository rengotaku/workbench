package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// The long-run band carries the next step (the open child highest in the
// queue) and the child counts; long-run tasks are not in the queue or counts.
func TestList_LongCarriesNextStepAndChildCounts(t *testing.T) {
	store, h := setup(t)
	parent, _ := store.Add("切替ドキュメント", "", "", task.ActorHuman)
	other, _ := store.Add("ほか", "", "", task.ActorHuman)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+parent.ID+"/long", map[string]any{"on": true})
	require.Equal(t, http.StatusOK, code)

	_, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "後の子", "parent": parent.ID})
	later := idOf(t, body)
	_, body = call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "済んだ子", "parent": parent.ID})
	doneKid := idOf(t, body)
	_, err := store.Done(doneKid, task.ActorHuman)
	require.NoError(t, err)
	_, body = call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "先の子", "parent": parent.ID})
	first := idOf(t, body)
	_, err = store.Reorder(first, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)

	_, body = call(t, h, http.MethodGet, "/api/tasks", nil)
	long := rowsOf(t, body, "long")
	require.Len(t, long, 1)
	row := asMap(t, long[0])
	assert.Equal(t, parent.ID, row["id"])
	assert.EqualValues(t, 1, row["kids_done"])
	assert.EqualValues(t, 2, row["kids_open"])
	next := asMap(t, row["next"])
	assert.Equal(t, first, next["id"], "the open child highest in the queue")

	ids := []any{}
	for _, r := range rowsOf(t, body, "queue") {
		ids = append(ids, asMap(t, r)["id"])
	}
	assert.Equal(t, []any{first, other.ID, later}, ids, "the long-run task is not in the queue")
	assert.EqualValues(t, 3, asMap(t, body["counts"])["rest"])
}

func TestList_LongWithoutOpenChildHasNoNext(t *testing.T) {
	store, h := setup(t)
	parent, _ := store.Add("親", "", "", task.ActorHuman)
	_, err := store.SetLong(parent.ID, true, task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
	row := asMap(t, rowsOf(t, body, "long")[0])
	_, has := row["next"]
	assert.False(t, has)
}

// A bad parent refuses the add, so nothing is added without its parent.
func TestAdd_WithABadParentAddsNothing(t *testing.T) {
	store, h := setup(t)
	plain, _ := store.Add("ふつう", "", "", task.ActorHuman)
	_, err := store.Done(plain.ID, task.ActorHuman)
	require.NoError(t, err)

	code, _ := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "子", "parent": plain.ID})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "子", "parent": "T999"})
	assert.Equal(t, http.StatusNotFound, code)

	tasks, _ := store.List()
	assert.Len(t, tasks, 1)
}

func TestPatch_ParentTiesAndUnties(t *testing.T) {
	store, h := setup(t)
	parent, _ := store.Add("親", "", "", task.ActorHuman)
	kid, _ := store.Add("子", "", "", task.ActorHuman)
	_, err := store.SetLong(parent.ID, true, task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+kid.ID, map[string]any{"parent": parent.ID})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, parent.ID, taskOf(t, body)["parent"])

	code, body = call(t, h, http.MethodPatch, "/api/tasks/"+kid.ID, map[string]any{"parent": ""})
	require.Equal(t, http.StatusOK, code)
	_, has := taskOf(t, body)["parent"]
	assert.False(t, has)
}

func TestLong_RequiresOn(t *testing.T) {
	store, h := setup(t)
	x, _ := store.Add("x", "", "", task.ActorHuman)
	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+x.ID+"/long", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+x.ID+"/long", map[string]any{"on": false})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "open", taskOf(t, body)["state"])
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "not an object: %v", v)
	return m
}
