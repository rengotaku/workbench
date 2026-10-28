package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// Every row, not only long-run ones, carries its parent's name and path and
// its children's counts (ADR todo-0020).
func TestList_EveryRowCarriesParentPathAndKidCounts(t *testing.T) {
	store, h := setup(t)
	top, _ := store.Add("長い仕事", "", "", task.ActorHuman)
	_, err := store.SetLong(top.ID, true, task.ActorHuman)
	require.NoError(t, err)
	main, err := store.AddChild("会議で共有", "", "", top.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	prep, err := store.AddChild("資料を作る", "", "", main.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	done, err := store.AddChild("下書き", "", "", main.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Done(done.ID, task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
	byID := map[string]map[string]any{}
	for _, sec := range []string{"queue", "long", "done"} {
		for _, r := range rowsOf(t, body, sec) {
			m := asMap(t, r)
			id, ok := m["id"].(string)
			require.True(t, ok)
			byID[id] = m
		}
	}

	m := byID[main.ID]
	assert.EqualValues(t, 1, m["kids_done"])
	assert.EqualValues(t, 1, m["kids_open"])
	assert.Equal(t, "長い仕事", m["parent_name"])
	path := listOf(t, m["parent_path"])
	require.Len(t, path, 1)
	assert.Equal(t, true, asMap(t, path[0])["long"])

	p := byID[prep.ID]
	assert.Equal(t, "会議で共有", p["parent_name"])
	path = listOf(t, p["parent_path"])
	require.Len(t, path, 2, "from the top down to the parent")
	assert.Equal(t, top.ID, asMap(t, path[0])["id"])
	assert.Equal(t, main.ID, asMap(t, path[1])["id"])
	_, has := asMap(t, path[1])["long"]
	assert.False(t, has, "only a long-run task is marked")
	_, has = p["kids_open"]
	assert.False(t, has, "no children, no counts")

	assert.EqualValues(t, 1, byID[top.ID]["kids_open"], "the long row keeps its counts")
}

func TestAPI_DueRuleIsA400WithTheMessage(t *testing.T) {
	store, h := setup(t)
	parent, _ := store.Add("親", "", dateIn(3), task.ActorHuman)
	kid, err := store.AddChild("子", "", dateIn(1), parent.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+kid.ID, map[string]any{"due": dateIn(3)})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "より前にする")

	code, body = call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "別の子", "parent": parent.ID, "due": dateIn(4)})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "親 "+parent.ID)
}

func TestAPI_OrderRuleIs409AndLeavesTheListAlone(t *testing.T) {
	store, h := setup(t)
	parent, _ := store.Add("親", "", "", task.ActorHuman)
	_, err := store.Add("A", "", "", task.ActorHuman)
	require.NoError(t, err)
	kid, err := store.AddChild("子", "", "", parent.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err) // 子 親 A

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+kid.ID+"/order", map[string]any{"to": 2})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, true, body["rule"])
	assert.NotEmpty(t, body["error"])

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	assert.Equal(t, kid.ID, asMap(t, rowsOf(t, list, "queue")[0])["id"])
}

func listOf(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "not a list: %v", v)
	return l
}
