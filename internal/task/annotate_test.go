package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func TestAnnotate_AppendsAndRecordsWhoWroteIt(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("調べる", "", "", task.ActorHuman)
	require.NoError(t, err)

	a, err := s.Annotate(added.ID, task.AnnotationInput{Kind: task.AnnotationQuote, Body: "引用", Source: " slack "}, task.ActorAI)
	require.NoError(t, err)
	assert.Equal(t, 1, a.ID)
	assert.Equal(t, task.ActorAI, a.Author)
	assert.Equal(t, "slack", a.Source)
	b, err := s.Annotate(added.ID, task.AnnotationInput{Kind: task.AnnotationMemo, Body: "二つ目"}, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, 2, b.ID)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	require.Len(t, got.Annotations, 2)
	assert.Equal(t, a, got.Annotations[0])
}

func TestAnnotate_RejectsBadInput(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Annotate(added.ID, task.AnnotationInput{Kind: "bogus", Body: "b"}, task.ActorAI)
	assert.Error(t, err)
	_, err = s.Annotate(added.ID, task.AnnotationInput{Kind: task.AnnotationMemo, Body: "  "}, task.ActorAI)
	assert.Error(t, err)
	_, err = s.Annotate("T999", task.AnnotationInput{Kind: task.AnnotationMemo, Body: "b"}, task.ActorAI)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

// An annotation is not something the person did, so undo walks past it to the
// last change they did make and leaves the supplement in place.
func TestUndo_SkipsAnnotationsAndKeepsThem(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("元の名前", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetName(added.ID, "直した名前", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Annotate(added.ID, task.AnnotationInput{Kind: task.AnnotationAnalysis, Body: "AI の補足"}, task.ActorAI)
	require.NoError(t, err)

	n, err := s.UndoableCount()
	require.NoError(t, err)
	assert.Equal(t, 2, n, "the annotation is not counted")

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindRenamed, done.Kind)
	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, "元の名前", got.Name)
	require.Len(t, got.Annotations, 1)
	assert.Equal(t, "AI の補足", got.Annotations[0].Body)
}

// Undoing a shelve restores the snapshot taken before it; supplements added in
// between must survive that.
func TestUndo_ShelveRestoreKeepsLaterAnnotations(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Wait(added.ID, "誰か", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Shelve(added.ID, dateIn(5), task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Annotate(added.ID, task.AnnotationInput{Kind: task.AnnotationMemo, Body: "keep"}, task.ActorAI)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindShelved, done.Kind)
	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateWaiting, got.State)
	require.Len(t, got.Annotations, 1)
}

func TestAddFromNote_ReusesTheTaskForTheSameNote(t *testing.T) {
	s := newStore(t)
	first, err := s.AddFromNote("a", "d", "", 7, task.ActorAI)
	require.NoError(t, err)
	assert.Equal(t, 7, first.SourceNoteID)

	again, err := s.AddFromNote("a", "d", "", 7, task.ActorAI)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	tasks, _ := s.List()
	assert.Len(t, tasks, 1)

	found, ok := s.FindBySourceNote(7)
	require.True(t, ok)
	assert.Equal(t, first.ID, found.ID)
	_, ok = s.FindBySourceNote(8)
	assert.False(t, ok)
}
