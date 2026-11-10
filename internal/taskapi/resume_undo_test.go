package taskapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// resume answers with the event it wrote, and POST /api/undo?event= undoes that
// resume only: after another change it answers 409 and touches nothing.
func TestResume_UndoByEventRefusesAfterAnotherChange(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("渡す", "", "", task.ActorHuman)
	other, _ := store.Add("別", "", "", task.ActorHuman)
	_, err := store.Wait(added.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)

	code, body := call(t, h, http.MethodPost, "/api/tasks/"+added.ID+"/resume", map[string]any{})
	require.Equal(t, http.StatusOK, code)
	ev, ok := body["event_id"].(string)
	require.True(t, ok, "resume names its event")
	assert.Equal(t, "相手", body["handed_to"])
	assert.NotEmpty(t, body["handed_on"])
	assert.EqualValues(t, store.Revision(), body["revision"])

	_, err = store.SetName(other.ID, "別2", task.ActorAI) // a change from the terminal
	require.NoError(t, err)

	code, _ = call(t, h, http.MethodPost, "/api/undo?event="+ev, nil)
	assert.Equal(t, http.StatusConflict, code)
	got, _ := store.Get(other.ID)
	assert.Equal(t, "別2", got.Name, "the later change was not undone in its place")
	got, _ = store.Get(added.ID)
	assert.Equal(t, task.StateOpen, got.State, "the resume was not undone either")

	_, err = store.Undo(task.ActorAI) // the rename
	require.NoError(t, err)
	code, _ = call(t, h, http.MethodPost, "/api/undo?event="+ev, nil)
	require.Equal(t, http.StatusOK, code)
	got, _ = store.Get(added.ID)
	assert.Equal(t, task.StateWaiting, got.State)
	assert.Equal(t, "相手", got.HandedTo)
}

// ?event= with no value, or given twice, is a 400 and changes nothing: it must
// not fall back to undoing whatever came last.
func TestUndo_EmptyOrRepeatedEventIsRefused(t *testing.T) {
	store, h := setup(t)
	added, _ := store.Add("残る", "", "", task.ActorHuman)
	rev := store.Revision()

	for _, q := range []string{"?event=", "?event=E0001&event=E0002", "?event"} {
		code, _ := call(t, h, http.MethodPost, "/api/undo"+q, nil)
		assert.Equal(t, http.StatusBadRequest, code, q)
	}
	_, err := store.Get(added.ID)
	assert.NoError(t, err, "the task was not undone away")
	assert.Equal(t, rev, store.Revision(), "nothing was written")
}
