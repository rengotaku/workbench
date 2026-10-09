package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// Focusing is a working set, not a state: the task keeps its place in the queue.
func TestSetFocus_DoesNotMoveTheTask(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	_, _ = s.Add("C", "", "", task.ActorHuman)

	focused, err := s.SetFocus(b.ID, true, task.ActorHuman)
	require.NoError(t, err)
	assert.True(t, focused.Focused)
	assert.Equal(t, 1, focused.Order, "it stays second in the queue")

	off, err := s.SetFocus(b.ID, false, task.ActorHuman)
	require.NoError(t, err)
	assert.False(t, off.Focused)
	assert.Equal(t, 1, off.Order)
}

func TestSetFocus_IsRecordedAndIdempotent(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)

	_, err := s.SetFocus(added.ID, true, task.ActorHuman)
	require.NoError(t, err)

	before := s.Revision()
	_, err = s.SetFocus(added.ID, true, task.ActorHuman) // already focused
	require.NoError(t, err)
	assert.Equal(t, before, s.Revision(), "setting the same value records nothing")

	events, err := s.Events(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.KindFocused, events[len(events)-1].Kind)
}

// Finishing a task takes it out of the band: the band is for what is next.
func TestDone_ClearsFocus(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err := s.SetFocus(added.ID, true, task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.False(t, done.Focused)
}

func TestSetFocus_RefusesACompletedTask(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetFocus(added.ID, true, task.ActorHuman)
	assert.ErrorContains(t, err, "is done")

	_, err = s.SetFocus("T999", true, task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestUndo_ReversesFocus(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err := s.SetFocus(added.ID, true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.False(t, got.Focused)
}
