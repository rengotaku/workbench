package task_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func TestSetLong_SetsAReviewDateAWeekOut(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A")
	got, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, dateIn(7), got.ReviewOn)
	assert.Nil(t, got.ReviewedAt, "making it long is not a review")

	// An existing date is kept.
	_, err = s.Review(id["A"], dateIn(30), task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetLong(id["A"], false, task.ActorHuman)
	require.NoError(t, err)
	again, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, dateIn(30), again.ReviewOn)
}

func TestReview_DefaultsToAWeekAndOnlyLongRunTasks(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B")
	_, err := s.Review(id["A"], "", task.ActorHuman)
	require.Error(t, err, "a queue task is not reviewed")

	_, err = s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	got, err := s.Review(id["A"], "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, dateIn(7), got.ReviewOn)
	require.NotNil(t, got.ReviewedAt)

	_, err = s.Review(id["A"], "next week", task.ActorHuman)
	require.Error(t, err)
}

func TestUndo_ReviewPutsBackTheDateAndTheTime(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A")
	_, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Review(id["A"], dateIn(3), task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Review(id["A"], dateIn(14), task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, _ := s.Get(id["A"])
	assert.Equal(t, dateIn(3), got.ReviewOn)
	require.NotNil(t, got.ReviewedAt, "the first review's time is back")

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, _ = s.Get(id["A"])
	assert.Equal(t, dateIn(7), got.ReviewOn)
	assert.Nil(t, got.ReviewedAt)
}

func TestUndo_LongTakesItsReviewDateBack(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A")
	_, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, _ := s.Get(id["A"])
	assert.Empty(t, got.ReviewOn)
}

// Data written before the fields existed loads, and a long-run task without a
// date is left empty (the API counts that as due).
func TestOpen_OldLongTaskHasNoReviewDate(t *testing.T) {
	dir := t.TempDir()
	old := `{"tasks":[{"id":"T001","name":"古い長期","state":"long","order":0,"created_at":"2026-10-01T09:00:00+09:00"}],"next_task":2,"next_event":1,"revision":1}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(old), 0o600))
	s := openAt(t, dir)
	got, err := s.Get("T001")
	require.NoError(t, err)
	assert.Empty(t, got.ReviewOn)
	assert.Nil(t, got.ReviewedAt)
}
