package task_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func TestSetTopic_SetsTrimsClearsAndSkipsNoOp(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)

	got, err := s.SetTopic(added.ID, "  青環離 ", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "青環離", got.Topic)

	_, err = s.SetTopic(added.ID, "青環離", task.ActorHuman)
	require.NoError(t, err)
	evs, err := s.Events(added.ID)
	require.NoError(t, err)
	assert.Len(t, evs, 2, "created + one topic_set; the unchanged value records nothing")
	assert.Equal(t, task.KindTopicSet, evs[1].Kind)

	got, err = s.SetTopic(added.ID, "  ", task.ActorHuman)
	require.NoError(t, err)
	assert.Empty(t, got.Topic)
}

func TestSetTopic_LengthLimitCountsCharacters(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)

	ok := strings.Repeat("話", task.MaxTopicLen)
	got, err := s.SetTopic(added.ID, ok, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, ok, got.Topic)

	_, err = s.SetTopic(added.ID, ok+"題", task.ActorHuman)
	require.Error(t, err)
	_, err = s.SetTopic(added.ID, "a\nb", task.ActorHuman)
	require.Error(t, err)
	now, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, ok, now.Topic, "a refused topic leaves the old one")
}

func TestSetTopic_UnknownTask(t *testing.T) {
	s := newStore(t)
	_, err := s.SetTopic("T999", "x", task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestSetTopic_UndoRestoresIncludingNoTopic(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("x", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetTopic(added.ID, "A", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetTopic(added.ID, "B", task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindTopicSet, done.Kind)
	got, _ := s.Get(added.ID)
	assert.Equal(t, "A", got.Topic)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, _ = s.Get(added.ID)
	assert.Empty(t, got.Topic, "undoing the first set puts back 'no topic'")
}
