package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// The list carries idle_days, idle_stage and idle_from, and leaves them out
// when there is nothing to show; an AI annotation does not reset the count.
func TestList_IdleFields(t *testing.T) {
	store, h := setup(t)
	late, _ := store.Add("遅れ", "", dateIn(-3), task.ActorHuman)
	fresh, _ := store.Add("新しい", "", "", task.ActorHuman)
	_, err := store.Annotate(late.ID, task.AnnotationInput{Kind: task.AnnotationMemo, Body: "AI"}, task.ActorAI)
	require.NoError(t, err)

	_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
	rows := map[string]map[string]any{}
	for _, r := range rowsOf(t, body, "queue") {
		m := asMap(t, r)
		id, ok := m["id"].(string)
		require.True(t, ok)
		rows[id] = m
	}
	assert.EqualValues(t, 3, rows[late.ID]["idle_days"])
	assert.EqualValues(t, 3, rows[late.ID]["idle_stage"])
	assert.Equal(t, "due", rows[late.ID]["idle_from"])
	_, has := rows[fresh.ID]["idle_days"]
	assert.False(t, has, "created today: nothing")

	_, err = store.Annotate(late.ID, task.AnnotationInput{Kind: task.AnnotationMemo, Body: "人"}, task.ActorHuman)
	require.NoError(t, err)
	_, body = call(t, h, http.MethodGet, "/api/tasks", nil)
	for _, r := range rowsOf(t, body, "queue") {
		if m := asMap(t, r); m["id"] == late.ID {
			_, has := m["idle_days"]
			assert.False(t, has, "a human note today resets it")
		}
	}
}

// Review through the API: the date moves a week out, review_due and stalled
// clear, and no_next stays while there is no open child.
func TestReview_ThroughTheAPI(t *testing.T) {
	store, h := setup(t)
	x, _ := store.Add("長い仕事", "", "", task.ActorHuman)
	other, _ := store.Add("ふつう", "", "", task.ActorHuman)
	code, _ := call(t, h, http.MethodPost, "/api/tasks/"+x.ID+"/long", map[string]any{"on": true})
	require.Equal(t, http.StatusOK, code)

	longRow := func() map[string]any {
		_, body := call(t, h, http.MethodGet, "/api/tasks", nil)
		return asMap(t, rowsOf(t, body, "long")[0])
	}
	row := longRow()
	assert.Equal(t, dateIn(7), row["review_on"], "becoming long sets a date a week out")
	assert.Equal(t, []any{"no_next"}, row["review_reasons"])

	_, err := store.Review(x.ID, dateIn(-1), task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []any{"no_next", "review_due"}, longRow()["review_reasons"])

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+x.ID+"/review", map[string]any{})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, dateIn(7), taskOf(t, body)["review_on"])
	row = longRow()
	assert.Equal(t, []any{"no_next"}, row["review_reasons"], "the alert stays while there is no next action")
	assert.Equal(t, true, row["needs_review"])

	_, err = store.AddChild("次", "", "", x.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	row = longRow()
	assert.Equal(t, false, row["needs_review"])
	assert.NotContains(t, row, "review_reasons")

	// A task that is not long-run is never reviewed and never carries the fields.
	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+other.ID+"/review", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = call(t, h, http.MethodPost, "/api/tasks/"+x.ID+"/review", map[string]any{"review_on": "soon"})
	assert.Equal(t, http.StatusBadRequest, code)
	_, list := call(t, h, http.MethodGet, "/api/tasks", nil)
	for _, r := range rowsOf(t, list, "queue") {
		assert.NotContains(t, asMap(t, r), "needs_review")
	}
}
