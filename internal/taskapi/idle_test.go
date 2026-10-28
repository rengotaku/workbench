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
