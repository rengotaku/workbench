package noteroute_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteroute"
	"github.com/rengotaku/workbench/internal/task"
)

func init() { gin.SetMode(gin.TestMode) }

type rig struct {
	notes   *note.Store
	tasks   *task.Store
	h       http.Handler
	noteDir string
	taskDir string
}

func setup(t *testing.T) rig {
	t.Helper()
	noteDir := t.TempDir()
	ns, err := note.Open(noteDir, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ns.Close() })
	taskDir := t.TempDir()
	ts, err := task.Open(taskDir, time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ts.Close() })
	r := gin.New()
	noteroute.New(ns, ts, time.Local).Mount(r)
	return rig{notes: ns, tasks: ts, h: r, noteDir: noteDir, taskDir: taskDir}
}

func (r rig) post(t *testing.T, id string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/notes/"+id+"/route", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return rec.Code, out
}

// m narrows a decoded JSON value to an object, failing the test's assertion
// (not panicking) is left to the callers' Equal checks.
func m(v any) map[string]any {
	o, _ := v.(map[string]any)
	return o
}

func todoBody(name string) map[string]any {
	return map[string]any{"to": "todo", "task": map[string]any{"name": name, "due": "2026-12-01", "detail": "d"}}
}

func TestRoute_RecordMarksTheNoteTriaged(t *testing.T) {
	r := setup(t)
	n, err := r.notes.Add("memo", "")
	require.NoError(t, err)

	code, out := r.post(t, "1", map[string]any{"to": "record"})
	require.Equal(t, http.StatusOK, code, out)
	got, _ := r.notes.Get(n.ID)
	assert.False(t, got.Pending())
	assert.Equal(t, note.RouteRecord, got.Route.To)
	assert.Zero(t, r.notes.PendingCount())
}

func TestRoute_TodoCreatesTheTaskAsAIFromTheNote(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)

	code, out := r.post(t, "1", todoBody("やる"))
	require.Equal(t, http.StatusOK, code, out)
	tk := m(out["task"])
	assert.Equal(t, "やる", tk["name"])
	assert.EqualValues(t, 1, tk["source_note_id"])
	assert.Equal(t, "2026-12-01", tk["due"])
	assert.Equal(t, "d", tk["detail"])
	nt := m(out["note"])
	assert.Equal(t, tk["id"], m(nt["route"])["task_id"])
	assert.NotNil(t, nt["triaged_at"])

	id, _ := tk["id"].(string)
	events, err := r.tasks.Events(id)
	require.NoError(t, err)
	assert.Equal(t, task.ActorAI, events[0].Actor)
}

func TestRoute_RerunWithTheSameDestinationIsANoOp(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)
	_, first := r.post(t, "1", todoBody("やる"))

	code, again := r.post(t, "1", todoBody("別の名前でも"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, m(first["task"])["id"], m(again["task"])["id"])
	tasks, _ := r.tasks.List()
	assert.Len(t, tasks, 1)
}

func TestRoute_AnotherDestinationIs409(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)
	r.post(t, "1", map[string]any{"to": "record"})

	code, _ := r.post(t, "1", todoBody("やる"))
	assert.Equal(t, http.StatusConflict, code)
	tasks, _ := r.tasks.List()
	assert.Empty(t, tasks, "a refused route must not make a task")

	_, err = r.notes.Add("memo2", "")
	require.NoError(t, err)
	r.post(t, "2", todoBody("x"))
	code, _ = r.post(t, "2", map[string]any{"to": "record"})
	assert.Equal(t, http.StatusConflict, code)
}

// The memo update fails after the task was made; the retry must reuse that
// task instead of making a second.
func TestRoute_RetryAfterNoteUpdateFailureReusesTheTask(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)

	// A directory where notes.json should be makes the atomic replace fail.
	path := filepath.Join(r.noteDir, "notes.json")
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0o700))

	code, _ := r.post(t, "1", todoBody("やる"))
	require.Equal(t, http.StatusInternalServerError, code)
	tasks, _ := r.tasks.List()
	require.Len(t, tasks, 1)
	got, _ := r.notes.Get(1)
	assert.True(t, got.Pending(), "the memo is still not picked up")

	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, saved, 0o600))

	code, out := r.post(t, "1", todoBody("やる"))
	require.Equal(t, http.StatusOK, code, out)
	tasks, _ = r.tasks.List()
	assert.Len(t, tasks, 1, "no second task")
	assert.Equal(t, tasks[0].ID, m(out["task"])["id"])
}

func TestRoute_Validation(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)

	for name, body := range map[string]any{
		"bad to":  map[string]any{"to": "x"},
		"no task": map[string]any{"to": "todo"},
		"blank":   map[string]any{"to": "todo", "task": map[string]any{"name": " "}},
		"bad due": map[string]any{"to": "todo", "task": map[string]any{"name": "a", "due": "12/1"}},
	} {
		code, _ := r.post(t, "1", body)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
	code, _ := r.post(t, "9", map[string]any{"to": "record"})
	assert.Equal(t, http.StatusNotFound, code)
	code, _ = r.post(t, "abc", map[string]any{"to": "record"})
	assert.Equal(t, http.StatusBadRequest, code)
	tasks, _ := r.tasks.List()
	assert.Empty(t, tasks)
}

// Saving the new task fails, the task stays in memory only. The retry must save
// it again before the memo points at it, so both survive a reopen.
func TestRoute_RetryAfterTaskSaveFailureSavesTheTask(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)

	path := filepath.Join(r.taskDir, "tasks.json")
	require.NoError(t, os.Mkdir(path, 0o700))
	code, _ := r.post(t, "1", todoBody("やる"))
	require.Equal(t, http.StatusInternalServerError, code)
	got, _ := r.notes.Get(1)
	require.True(t, got.Pending(), "the memo must not point at an unsaved task")
	require.NoError(t, os.Remove(path))

	code, out := r.post(t, "1", todoBody("やる"))
	require.Equal(t, http.StatusOK, code, out)

	require.NoError(t, r.notes.Close())
	require.NoError(t, r.tasks.Close())
	ns, err := note.Open(r.noteDir, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ns.Close() })
	ts, err := task.Open(r.taskDir, time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ts.Close() })
	n, _ := ns.Get(1)
	require.NotNil(t, n.Route)
	tk, err := ts.Get(n.Route.TaskID)
	require.NoError(t, err)
	assert.Equal(t, 1, tk.SourceNoteID)
}

// The memo update failed after the task was made; a record request must not
// turn the memo into a record while a task exists for it.
func TestRoute_RecordIsRefusedWhenATaskAlreadyExistsForTheNote(t *testing.T) {
	r := setup(t)
	_, err := r.notes.Add("memo", "")
	require.NoError(t, err)
	_, err = r.tasks.AddFromNote("やる", "", "", 1, task.ActorAI)
	require.NoError(t, err)

	code, _ := r.post(t, "1", map[string]any{"to": "record"})
	assert.Equal(t, http.StatusConflict, code)
	got, _ := r.notes.Get(1)
	assert.True(t, got.Pending())
}
