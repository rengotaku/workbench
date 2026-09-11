package task_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/todo-order/internal/task"
)

func TestUndo_WalksBackwardsOneChangeAtATime(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("元の名前", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetName(added.ID, "直した名前", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetDue(added.ID, dateIn(3), task.ActorHuman)
	require.NoError(t, err)

	// Newest first: the due date goes back.
	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindDueSet, done.Kind)
	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Due)
	assert.Equal(t, "直した名前", got.Name)

	// Then the rename.
	done, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindRenamed, done.Kind)
	got, err = s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, "元の名前", got.Name)

	// Then the task itself.
	done, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindCreated, done.Kind)
	_, err = s.Get(added.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)

	_, err = s.Undo(task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNothingToUndo)
}

// Undoing must not toggle the same change forever: each reversal is spent.
func TestUndo_DoesNotRepeatTheSameChange(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindDone, done.Kind)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, got.State)

	// The next undo steps past it, to the creation.
	done, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindCreated, done.Kind)
}

func TestUndo_PutsARemovedTaskBack(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add("残る", "", "", task.ActorHuman)
	junk, _ := s.Add("誤って消した", "詳しい話", dateIn(2), task.ActorHuman)

	_, err := s.Remove(junk.ID, task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindRemoved, done.Kind)

	back, err := s.Get(junk.ID)
	require.NoError(t, err)
	assert.Equal(t, "誤って消した", back.Name)
	assert.Equal(t, "詳しい話", back.Detail, "the detail comes back too")
	assert.Equal(t, dateIn(2), back.Due)
	assert.Equal(t, 1, back.Order, "it returns at the end of the queue")
}

func TestUndo_RestoresTheDetailItReplaced(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "はじめの詳細", "", task.ActorHuman)
	_, err := s.SetDetail(added.ID, "書き換えた詳細", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, "はじめの詳細", got.Detail)
}

func TestUndo_ReversesAReorder(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	c, _ := s.Add("C", "", "", task.ActorHuman)

	_, err := s.Reorder(c.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	tasks, _ := s.List()
	order := map[string]int{}
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[a.ID])
	assert.Equal(t, 1, order[b.ID])
	assert.Equal(t, 2, order[c.ID], "it goes back where it was")
}

func TestUndo_ReversesAHandoffAndAResume(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("渡す", "", "", task.ActorHuman)

	_, err := s.Wait(added.ID, "相手", "回答待ち", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, got.State)
	assert.Empty(t, got.HandedTo)

	// Undoing a resume puts it back with whoever held it.
	_, err = s.Wait(added.ID, "別の相手", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Resume(added.ID, "返ってきた", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err = s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateWaiting, got.State)
	assert.Equal(t, "別の相手", got.HandedTo)
}

// An undo is itself a change, so the history keeps growing rather than losing
// what happened.
func TestUndo_IsRecordedInTheHistory(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err := s.SetName(added.ID, "直した", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorAI)
	require.NoError(t, err)

	events, err := s.Events(added.ID)
	require.NoError(t, err)
	require.Len(t, events, 3, "created, renamed, and the rename back")
	assert.Equal(t, task.KindRenamed, events[2].Kind)
	assert.Equal(t, task.ActorAI, events[2].Actor)
	assert.Equal(t, "A", events[2].ToValue)
}

func TestUndo_SkipsAChangeWhoseTaskIsGone(t *testing.T) {
	s := newStore(t)
	keep, _ := s.Add("残る", "", "", task.ActorHuman)
	gone, _ := s.Add("消える", "", "", task.ActorHuman)

	_, err := s.SetName(gone.ID, "消える（改名後）", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetName(keep.ID, "残る（改名後）", task.ActorHuman)
	require.NoError(t, err)

	// Remove the task whose rename sits in the middle of the stack.
	_, err = s.Remove(gone.ID, task.ActorHuman)
	require.NoError(t, err)

	// First undo puts the removed task back.
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	// Then the rename of the surviving task.
	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, keep.ID, done.TaskID)

	got, err := s.Get(keep.ID)
	require.NoError(t, err)
	assert.Equal(t, "残る", got.Name)
}

func TestUndoableCount_CountsWhatIsLeft(t *testing.T) {
	s := newStore(t)
	n, err := s.UndoableCount()
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err = s.SetDue(added.ID, dateIn(1), task.ActorHuman)
	require.NoError(t, err)

	n, err = s.UndoableCount()
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	n, err = s.UndoableCount()
	require.NoError(t, err)
	assert.Equal(t, 1, n, "both the reversed change and the reversal itself are spent")
}

func TestUndo_SurvivesAReload(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	added, _ := s.Add("A", "", "", task.ActorHuman)
	_, err = s.SetName(added.ID, "直した", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	// A fresh store must not offer the same reversal again: what is left is the
	// creation, one step further back.
	reloaded, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	done, err := reloaded.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindCreated, done.Kind)

	_, err = reloaded.Get(added.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)
}
