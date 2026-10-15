package taskapi_test

import (
	"bytes"
	"encoding/json"
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

func init() { gin.SetMode(gin.TestMode) }

func setup(t *testing.T) (*task.Store, http.Handler) {
	t.Helper()
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)

	r := gin.New()
	taskapi.New(store, time.Local).Mount(r)
	return store, r
}

func dateIn(days int) string {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local).
		AddDate(0, 0, days).Format(task.DateLayout)
}

func call(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := map[string]any{}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	}
	return rec.Code, out
}

func TestList_SplitsSectionsAndCountsTheScope(t *testing.T) {
	store, h := setup(t)

	overdue, err := store.Add("超過している", "", dateIn(-1), task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Add("週内に期限", "", dateIn(3), task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Add("ずっと先", "", dateIn(60), task.ActorHuman)
	require.NoError(t, err)
	waiting, err := store.Add("渡してある", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Wait(waiting.ID, "相手", "回答待ち", task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodGet, "/api/tasks?scope=7", nil)
	require.Equal(t, http.StatusOK, code)

	queue := rowsOf(t, body, "queue")
	assert.Len(t, queue, 3)
	assert.Len(t, rowsOf(t, body, "waiting"), 1)

	counts := countsOf(t, body)
	assert.EqualValues(t, 1, counts["over"])
	assert.EqualValues(t, 0, counts["near"], "amber counts only today, the same as the row stripe")
	assert.EqualValues(t, 2, counts["rest"], "a date still ahead is uncoloured, in the week or not")

	first, ok := queue[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, overdue.ID, first["id"])
	assert.Equal(t, "over", first["due_class"])
	assert.Equal(t, "1日超過", first["due_label"])
	assert.Equal(t, true, first["overdue"])
}

// countsOf pulls the three header numbers out of a list response.
func countsOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	counts, ok := body["counts"].(map[string]any)
	require.True(t, ok, "response has no counts: %v", body)
	return counts
}

// rowsOf pulls one section out of a list response.
func rowsOf(t *testing.T, body map[string]any, key string) []any {
	t.Helper()
	rows, ok := body[key].([]any)
	if !ok {
		return nil
	}
	return rows
}

// taskOf pulls the task out of a single-task response.
func taskOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	got, ok := body["task"].(map[string]any)
	require.True(t, ok, "response has no task: %v", body)
	return got
}

// idOf reads the task ID out of a single-task response.
func idOf(t *testing.T, body map[string]any) string {
	t.Helper()
	id, ok := taskOf(t, body)["id"].(string)
	require.True(t, ok, "task has no id: %v", body)
	return id
}

func TestList_WideningTheScopePullsTheFarDueDateIn(t *testing.T) {
	store, h := setup(t)
	_, err := store.Add("10日後", "", dateIn(10), task.ActorHuman)
	require.NoError(t, err)

	inScope := func(body map[string]any) any {
		rows := rowsOf(t, body, "queue")
		require.Len(t, rows, 1)
		row, ok := rows[0].(map[string]any)
		require.True(t, ok)
		return row["in_scope"]
	}

	_, week := call(t, h, http.MethodGet, "/api/tasks?scope=7", nil)
	assert.Equal(t, false, inScope(week))

	_, fortnight := call(t, h, http.MethodGet, "/api/tasks?scope=14", nil)
	assert.Equal(t, true, inScope(fortnight))

	_, all := call(t, h, http.MethodGet, "/api/tasks?scope=all", nil)
	assert.Equal(t, true, inScope(all))
}

// The due date is when to start, so "today" and "past it" are told apart: the
// page shows today in amber and past in red, and the header counts it the same
// way. overdue stays true for both.
func TestList_DueTodayIsItsOwnClass(t *testing.T) {
	store, h := setup(t)
	_, err := store.Add("今日着手", "", dateIn(0), task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks?scope=7", nil)
	queue := rowsOf(t, body, "queue")
	require.Len(t, queue, 1)
	row, ok := queue[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "today", row["due_class"])
	assert.Equal(t, "今日", row["due_label"])
	assert.Equal(t, true, row["overdue"])
	assert.EqualValues(t, 0, countsOf(t, body)["over"])
	assert.EqualValues(t, 1, countsOf(t, body)["near"])
}

// A task with no due date is the top of the queue often enough that hiding it
// under "スコープ外" loses the first thing to work on.
func TestList_ATaskWithNoDueDateStaysVisible(t *testing.T) {
	store, h := setup(t)
	noDue, err := store.Add("期限なし", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Add("ずっと先", "", dateIn(60), task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks?scope=7", nil)

	rows := rowsOf(t, body, "queue")
	require.Len(t, rows, 2)

	first, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, noDue.ID, first["id"])
	assert.Equal(t, true, first["in_scope"], "no due date means nothing to be outside of")

	second, ok := rows[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, second["in_scope"], "a far due date is genuinely outside the window")

	// Both still count as "neither overdue nor due this week".
	assert.EqualValues(t, 2, countsOf(t, body)["rest"])
}

func TestList_RejectsAMeaninglessScope(t *testing.T) {
	_, h := setup(t)
	code, body := call(t, h, http.MethodGet, "/api/tasks?scope=soon", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "scope")
}

func TestList_ShowsOnlyThisWeeksCompletions(t *testing.T) {
	store, h := setup(t)
	fresh, err := store.Add("今週やった", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.Done(fresh.ID, task.ActorHuman)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
	assert.Len(t, rowsOf(t, body, "done"), 1)
}

func TestAdd_ThenDone_ReportsAConflictOnTheSecondTry(t *testing.T) {
	_, h := setup(t)

	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "やる"})
	require.Equal(t, http.StatusOK, code)
	id := idOf(t, body)

	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+id+"/done", nil)
	assert.Equal(t, http.StatusOK, code)

	code, body = call(t, h, http.MethodPost, "/api/tasks/"+id+"/done", nil)
	assert.Equal(t, http.StatusConflict, code)
	assert.Contains(t, body["error"], "already done")
}

func TestReopen_UndoesACompletionThroughTheAPI(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("誤って完了にした", "", "", task.ActorHuman)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/done", nil)
	require.Equal(t, http.StatusOK, code)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/reopen", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "open", taskOf(t, body)["state"])

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	assert.Len(t, rowsOf(t, list, "queue"), 1)
	assert.Empty(t, rowsOf(t, list, "done"))
}

func TestReopen_OnATaskThatIsNotDoneIsRejected(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("まだ終わっていない", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/reopen", nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "not done")
}

func TestRemove_TakesATaskOutThroughTheAPI(t *testing.T) {
	store, h := setup(t)
	keep, _ := store.Add("残る", "", "", task.ActorHuman)
	junk, _ := store.Add("誤って追加した", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodDelete, "/api/tasks/"+junk.ID, nil)
	require.Equal(t, http.StatusOK, code)
	gone, ok := body["removed"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "誤って追加した", gone["name"])

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	rows := rowsOf(t, list, "queue")
	require.Len(t, rows, 1)
	first, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, keep.ID, first["id"])

	code, _ = call(t, h, http.MethodDelete, "/api/tasks/"+junk.ID, nil)
	assert.Equal(t, http.StatusNotFound, code, "removing it twice is not found")
}

func TestUnknownID_Is404(t *testing.T) {
	_, h := setup(t)
	code, _ := call(t, h, http.MethodPost, "/api/tasks/T999/done", nil)
	assert.Equal(t, http.StatusNotFound, code)

	code, _ = call(t, h, http.MethodGet, "/api/tasks/T999/context", nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestAdd_RejectsAnEmptyName(t *testing.T) {
	_, h := setup(t)
	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": ""})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.NotEmpty(t, body["error"])
}

func TestPatch_NeedsSomethingToChange(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("直す", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+added.ID, map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "nothing to change")

	code, _ = call(t, h, http.MethodPatch, "/api/tasks/"+added.ID,
		map[string]any{"due": dateIn(2), "detail": "詳しい話"})
	require.Equal(t, http.StatusOK, code)

	got, err := store.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, dateIn(2), got.Due)
	assert.Equal(t, "詳しい話", got.Detail)
}

// Everything the page needs to create and edit has to be reachable over the
// API, not only from the CLI.
func TestPatch_ChangesTheName(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("元の名前", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+added.ID,
		map[string]any{"name": "直した名前"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "直した名前", taskOf(t, body)["name"])
}

func TestPatch_RejectsAnEmptyName(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("名前", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPatch, "/api/tasks/"+added.ID,
		map[string]any{"name": ""})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "name is required")
}

func TestWait_RequiresWhoHoldsTheBall(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("渡す", "", "", task.ActorHuman)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/wait", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)

	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/wait",
		map[string]any{"to": "相手", "note": "回答待ち"})
	require.Equal(t, http.StatusOK, code)

	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/resume",
		map[string]any{"note": "返ってきた"})
	assert.Equal(t, http.StatusOK, code)
}

func TestOrder_MovesATaskAndTheEventNamesWhatItOvertook(t *testing.T) {
	store, h := setup(t)
	near, _ := store.Add("期限が近い", "", dateIn(2), task.ActorHuman)
	mover, _ := store.Add("期限なし", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+mover.ID+"/order",
		map[string]any{"to": 0, "scope_days": 7})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 0, taskOf(t, body)["order"])

	code, ctx := call(t, h, http.MethodGet, "/api/tasks/"+mover.ID+"/context", nil)
	require.Equal(t, http.StatusOK, code)

	var jumpedIDs []string
	for _, raw := range rowsOf(t, ctx, "events") {
		e, ok := raw.(map[string]any)
		require.True(t, ok)
		if e["kind"] != string(task.KindReordered) {
			continue
		}
		jumped, ok := e["jumped"].([]any)
		if !ok {
			continue
		}
		for _, j := range jumped {
			row, ok := j.(map[string]any)
			require.True(t, ok)
			id, ok := row["id"].(string)
			require.True(t, ok)
			jumpedIDs = append(jumpedIDs, id)
		}
	}
	assert.Equal(t, []string{near.ID}, jumpedIDs)
}

func TestOrder_RejectsAPositionOutsideTheQueue(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("1件だけ", "", "", task.ActorHuman)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/order",
		map[string]any{"to": 9})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, body["error"], "out of range")
}

// The page polls the revision to notice a change made by the CLI or another
// window without pulling the whole list.
func TestRevision_IsServedAndMatchesTheList(t *testing.T) {
	store, h := setup(t)
	_, err := store.Add("何か", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	_, rev := call(t, h, http.MethodGet, "/api/revision", nil)
	assert.Equal(t, list["revision"], rev["revision"])

	_, err = store.Add("もう一つ", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, after := call(t, h, http.MethodGet, "/api/revision", nil)
	assert.NotEqual(t, rev["revision"], after["revision"])
}

// The colours follow the date, which moves without any write. The revision
// carries the server's today so a page left open overnight reloads.
func TestRevision_CarriesTheSameTodayAsTheList(t *testing.T) {
	_, h := setup(t)

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	_, rev := call(t, h, http.MethodGet, "/api/revision", nil)
	assert.NotEmpty(t, rev["today"])
	assert.Equal(t, list["today"], rev["today"])
}

// A drag built on a list that has since moved comes back as a conflict, so the
// page can re-read instead of putting the row in the wrong place.
func TestOrder_AStaleRevisionIsAConflict(t *testing.T) {
	store, h := setup(t)
	a, _ := store.Add("A", "", "", task.ActorHuman)
	b, _ := store.Add("B", "", "", task.ActorHuman)
	_, _ = store.Add("C", "", "", task.ActorHuman)

	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	rawRev, ok := list["revision"].(float64)
	require.True(t, ok)
	stale := int(rawRev)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+b.ID+"/order",
		map[string]any{"to": 0, "revision": stale})
	require.Equal(t, http.StatusOK, code)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+a.ID+"/order",
		map[string]any{"to": 2, "revision": stale})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, true, body["stale"])
}

func TestAskAndWhy_QueueTheChangeThenRecordTheReason(t *testing.T) {
	store, h := setup(t)
	_, _ = store.Add("A", "", "", task.ActorHuman)
	b, _ := store.Add("B", "", "", task.ActorHuman)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+b.ID+"/order", map[string]any{"to": 0})
	require.Equal(t, http.StatusOK, code)

	code, body := call(t, h, http.MethodGet, "/api/ask", nil)
	require.Equal(t, http.StatusOK, code)
	pending := rowsOf(t, body, "pending")
	require.Len(t, pending, 1)

	first, ok := pending[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1位に上げた", first["trigger"])

	event, ok := first["event"].(map[string]any)
	require.True(t, ok)
	eventID, ok := event["id"].(string)
	require.True(t, ok)

	code, body = call(t, h, http.MethodPost, "/api/events/"+eventID+"/why",
		map[string]any{"reason": "切替日が動いたので前に出した"})
	require.Equal(t, http.StatusOK, code)
	recorded, ok := body["event"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "切替日が動いたので前に出した", recorded["reason"])

	code, body = call(t, h, http.MethodGet, "/api/ask", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, rowsOf(t, body, "pending"), "an explained change leaves the queue")
}

func TestWhy_UnknownEventIs404(t *testing.T) {
	_, h := setup(t)
	code, _ := call(t, h, http.MethodPost, "/api/events/E9999/why",
		map[string]any{"reason": "理由"})
	assert.Equal(t, http.StatusNotFound, code)
}

func TestActor_IsRecordedSoAIChangesAreDistinguishable(t *testing.T) {
	store, h := setup(t)

	code, body := call(t, h, http.MethodPost, "/api/tasks",
		map[string]any{"name": "AI が入れた", "actor": "ai"})
	require.Equal(t, http.StatusOK, code)
	id := idOf(t, body)

	events, err := store.Events(id)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, task.ActorAI, events[0].Actor)
}

// A task added from a pasted link carries the link through the API, and the
// same field can be changed or cleared later.
func TestAPI_AddAndPatchCarryTheURL(t *testing.T) {
	_, h := setup(t)

	code, body := call(t, h, http.MethodPost, "/api/tasks", map[string]any{
		"name":      "ABC-1 踏み台 EIP を確保する",
		"url":       "https://example.test/view/ABC-1",
		"link_text": "ABC-1",
	})
	require.Equal(t, http.StatusOK, code)
	got, ok := body["task"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.test/view/ABC-1", got["url"])
	assert.Equal(t, "ABC-1", got["link_text"]) // only the words that were a link

	id, _ := got["id"].(string)
	require.NotEmpty(t, id)

	code, body = call(t, h, http.MethodPatch, "/api/tasks/"+id, map[string]any{
		"url": "https://example.test/pages/42",
	})
	require.Equal(t, http.StatusOK, code)
	got, _ = body["task"].(map[string]any)
	assert.Equal(t, "https://example.test/pages/42", got["url"])

	code, body = call(t, h, http.MethodPatch, "/api/tasks/"+id, map[string]any{"url": ""})
	require.Equal(t, http.StatusOK, code)
	got, _ = body["task"].(map[string]any)
	assert.NotContains(t, got, "url") // an empty link is omitted, not kept as ""
	assert.NotContains(t, got, "link_text")
}

func TestAPI_PatchWithNothingToChangeIsRejected(t *testing.T) {
	_, h := setup(t)

	code, _ := call(t, h, http.MethodPost, "/api/tasks", map[string]any{"name": "何か"})
	require.Equal(t, http.StatusOK, code)

	code, _ = call(t, h, http.MethodPatch, "/api/tasks/T001", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)
}

// A drop into the band sends where it landed; the queue comes back with the
// focused block on top and the task at that place in it.
func TestFocus_TakesThePlaceItWasDroppedAt(t *testing.T) {
	store, h := setup(t)
	a, err := store.Add("A", "", "", task.ActorHuman)
	require.NoError(t, err)
	b, err := store.Add("B", "", "", task.ActorHuman)
	require.NoError(t, err)
	c, err := store.Add("C", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = store.SetFocus(a.ID, true, task.ActorHuman)
	require.NoError(t, err)

	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+c.ID+"/focus", map[string]any{"on": true, "to": 0})
	require.Equal(t, http.StatusOK, code)

	_, body := call(t, h, http.MethodGet, "/api/tasks?scope=7", nil)
	var order []any
	for _, r := range rowsOf(t, body, "queue") {
		row, ok := r.(map[string]any)
		require.True(t, ok)
		order = append(order, row["id"])
	}
	assert.Equal(t, []any{c.ID, a.ID, b.ID}, order)

	focus := rowsOf(t, body, "focus")
	require.Len(t, focus, 2)
	first, ok := focus[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, c.ID, first["id"])
}
