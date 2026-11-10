package task_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// waitingThreeDaysAgo is a store with one task handed on three days ago.
func waitingThreeDaysAgo(t *testing.T) (*task.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	s := openAt(t, dir)
	added, err := s.Add("A", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Wait(added.ID, "高橋", "", task.ActorHuman)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.NoError(t, backdateHandoff(dir, added.ID, 3))
	return openAt(t, dir), dir, added.ID
}

// Undoing a resume puts back who held it and the day it was handed on, not the
// day of the undo.
func TestUndo_ResumePutsBackTheHandoffDate(t *testing.T) {
	s, _, id := waitingThreeDaysAgo(t)

	_, err := s.Resume(id, "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(id)
	require.NoError(t, err)
	assert.Equal(t, task.StateWaiting, got.State)
	assert.Equal(t, "高橋", got.HandedTo)
	assert.Equal(t, dateIn(-3), got.HandedOn)
}

// A resume written before the event kept the date still undoes, with today.
func TestUndo_ResumeEventWithoutHandedOnUsesToday(t *testing.T) {
	s, dir, id := waitingThreeDaysAgo(t)
	_, err := s.Resume(id, "", task.ActorHuman)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	files, err := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	require.NoError(t, err)
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		require.NoError(t, rerr)
		out := regexp.MustCompile(`,"handed_on":"[^"]*"`).ReplaceAll(b, nil)
		require.NotEqual(t, string(b), string(out), "the event carried handed_on")
		require.NoError(t, os.WriteFile(f, out, 0o600))
	}

	re := openAt(t, dir)
	_, err = re.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, err := re.Get(id)
	require.NoError(t, err)
	assert.Equal(t, "高橋", got.HandedTo)
	assert.Equal(t, dateIn(0), got.HandedOn)
}

// UndoEvent reverses the resume it was given, and refuses once another change
// has come after it: the later change is left alone.
func TestUndoEvent_OnlyTheNamedChange(t *testing.T) {
	s, _, id := waitingThreeDaysAgo(t)
	other, err := s.Add("B", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, rec, err := s.ResumeWithEvent(id, "", task.ActorHuman)
	require.NoError(t, err)
	ev := rec.EventID
	assert.Equal(t, "高橋", rec.HandedTo)
	assert.Equal(t, dateIn(-3), rec.HandedOn)
	assert.Equal(t, s.Revision(), rec.Revision)
	_, err = s.SetName(other.ID, "B2", task.ActorAI)
	require.NoError(t, err)

	_, err = s.UndoEvent(ev, task.ActorHuman)
	require.ErrorIs(t, err, task.ErrUndoMismatch)
	got, err := s.Get(other.ID)
	require.NoError(t, err)
	assert.Equal(t, "B2", got.Name, "the later rename was not undone")
	got, err = s.Get(id)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, got.State, "the resume was not undone either")

	_, err = s.Undo(task.ActorHuman) // the rename
	require.NoError(t, err)
	done, err := s.UndoEvent(ev, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, ev, done.EventID)
	got, err = s.Get(id)
	require.NoError(t, err)
	assert.Equal(t, task.StateWaiting, got.State)
}

// An empty event ID is refused, not read as "undo the last change".
func TestUndoEvent_RefusesAnEmptyID(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("A", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.UndoEvent("", task.ActorHuman)
	require.ErrorIs(t, err, task.ErrUndoEventRequired)
	_, err = s.Get(added.ID)
	assert.NoError(t, err, "the task was not undone away")
}
