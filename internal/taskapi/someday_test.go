package taskapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
)

// setupWithDir is setup, plus the directory the store keeps tasks.json in.
// Only the tests below that need to force an empty Until onto disk (Shelve
// itself never leaves one) reach for it.
func setupWithDir(t *testing.T) (*task.Store, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	r := gin.New()
	taskapi.New(store, time.Local).Mount(r)
	return store, r, dir
}

// clearUntil rewrites tasks.json so one task's until is empty. Shelve always
// fills it in, so the only way to exercise the "empty until sorts last"
// branch is to force the on-disk state directly, the same way
// internal/task's helper_test.go backdates a handoff.
func clearUntil(t *testing.T, dir, id string) {
	t.Helper()
	path := filepath.Join(dir, "tasks.json")
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	var st struct {
		Tasks     []task.Task `json:"tasks"`
		NextTask  int         `json:"next_task"`
		NextEvent int         `json:"next_event"`
		Revision  int         `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(b, &st))

	found := false
	for i := range st.Tasks {
		if st.Tasks[i].ID == id {
			st.Tasks[i].Until = ""
			found = true
		}
	}
	require.True(t, found, "task %s not present in %s", id, path)

	out, err := json.MarshalIndent(st, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(out, '\n'), 0o600))
}

// 10. GET /api/tasks sorts someday by Until ascending, empty Until last.
func TestList_SomedaySortsByUntilWithEmptyLast(t *testing.T) {
	store, _, dir := setupWithDir(t)

	noUntil, _ := store.Add("日付なし", "", "", task.ActorHuman)
	late, _ := store.Add("遠いいつか", "", "", task.ActorHuman)
	soon, _ := store.Add("近いいつか", "", "", task.ActorHuman)

	_, err := store.Shelve(noUntil.ID, dateIn(1), task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Shelve(late.ID, dateIn(60), task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Shelve(soon.ID, dateIn(5), task.ActorHuman)
	require.NoError(t, err)

	clearUntil(t, dir, noUntil.ID)

	reloaded, err := task.Open(dir, time.Local)
	require.NoError(t, err)
	r := gin.New()
	taskapi.New(reloaded, time.Local).Mount(r)

	_, body := call(t, r, http.MethodGet, "/api/tasks", nil)
	someday := rowsOf(t, body, "someday")
	require.Len(t, someday, 3)

	var ids []string
	for _, raw := range someday {
		row, ok := raw.(map[string]any)
		require.True(t, ok)
		id, ok := row["id"].(string)
		require.True(t, ok)
		ids = append(ids, id)
	}
	assert.Equal(t, []string{soon.ID, late.ID, noUntil.ID}, ids,
		"soonest until first, empty until last")
}

// 11. until_reached is true for today or the past, false for the future.
func TestList_UntilReachedIsTrueOnOrBeforeToday(t *testing.T) {
	store, h := setup(t)

	pastDue, _ := store.Add("到来した", "", "", task.ActorHuman)
	_, err := store.Shelve(pastDue.ID, dateIn(-3), task.ActorHuman)
	require.NoError(t, err)

	exactlyToday, _ := store.Add("今日ちょうど", "", "", task.ActorHuman)
	_, err = store.Shelve(exactlyToday.ID, dateIn(0), task.ActorHuman)
	require.NoError(t, err)

	future, _ := store.Add("まだ先", "", "", task.ActorHuman)
	_, err = store.Shelve(future.ID, dateIn(10), task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
	reached := map[string]bool{}
	for _, raw := range rowsOf(t, body, "someday") {
		row, ok := raw.(map[string]any)
		require.True(t, ok)
		id, _ := row["id"].(string)
		reached[id], _ = row["until_reached"].(bool)
	}
	assert.True(t, reached[pastDue.ID])
	assert.True(t, reached[exactlyToday.ID], "today counts as reached")
	assert.False(t, reached[future.ID])
}

// 12. POST /api/tasks/{id}/someday without an until fills in "+1 month".
func TestSomeday_WithoutUntilDefaultsToOneMonthOut(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("いつか棚上げする", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/someday", map[string]any{})
	require.Equal(t, http.StatusOK, code)

	got := taskOf(t, body)
	assert.Equal(t, "someday", got["state"])
	until, ok := got["until"].(string)
	require.True(t, ok, "someday response has no until: %v", got)

	back, err := store.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, until, back.Until)
	assert.NotEmpty(t, back.Until)
}

// 13. POST /api/tasks/{id}/someday on an already-someday task extends it —
// the API is where a postpone during triage lands, and it must not need the
// two-step resume-then-shelve detour.
func TestSomeday_OnAnAlreadySomedayTaskExtendsIt(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("いつか延ばす（API）", "", "", task.ActorHuman)
	_, err := store.Shelve(added.ID, dateIn(5), task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/someday",
		map[string]any{"until": dateIn(20)})
	require.Equal(t, http.StatusOK, code)
	got := taskOf(t, body)
	assert.Equal(t, "someday", got["state"])
	assert.Equal(t, dateIn(20), got["until"])

	back, err := store.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateSomeday, back.State)
	assert.Equal(t, dateIn(20), back.Until)
}

// The API also accepts an explicit until, and resume still reaches a shelved
// task through the API the way it reaches a waiting one.
func TestSomeday_AnExplicitUntilIsKeptAndResumeReachesIt(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("指定日で棚上げ", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/someday",
		map[string]any{"until": dateIn(14)})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, dateIn(14), taskOf(t, body)["until"])

	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/resume", map[string]any{})
	require.Equal(t, http.StatusOK, code)

	back, err := store.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, back.State)
	assert.Empty(t, back.Until)
}
