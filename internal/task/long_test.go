package task_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// Long-run is a state (ADR todo-0019): the task leaves the queue and the focus
// band, and coming back puts it just below the focused block.
func TestSetLong_TakesTheTaskOutOfTheQueueAndBringsItBackOnTop(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C", "D")
	_, err := s.SetFocus(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(id["C"], true, task.ActorHuman)
	require.NoError(t, err)

	long, err := s.SetLong(id["C"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateLong, long.State)
	assert.False(t, long.Focused, "long-run does not keep a focus slot")
	assert.Equal(t, []string{"A", "B", "D"}, queueNames(t, s))

	back, err := s.SetLong(id["C"], false, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, back.State)
	assert.False(t, back.Focused)
	assert.Equal(t, []string{"A", "C", "B", "D"}, queueNames(t, s), "just below the focused block")
}

func TestSetLong_RefusesTasksOutsideTheQueueAndIsIdempotent(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B")
	_, err := s.Wait(id["A"], "someone", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetLong(id["A"], true, task.ActorHuman)
	require.Error(t, err, "a waiting task is not in the queue")

	_, err = s.SetLong(id["B"], false, task.ActorHuman)
	require.NoError(t, err, "unlonging a queue task changes nothing")

	_, err = s.SetLong(id["B"], true, task.ActorHuman)
	require.NoError(t, err)
	again, err := s.SetLong(id["B"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateLong, again.State)
}

func TestLongTask_CannotBeFocusedOrHandedOn(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A")
	_, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetFocus(id["A"], true, task.ActorHuman)
	require.Error(t, err)
	_, err = s.Wait(id["A"], "someone", "", task.ActorHuman)
	require.Error(t, err)
	_, err = s.Reorder(id["A"], 0, 7, 0, task.ActorHuman)
	require.Error(t, err)
}

func TestSetParent_OnlyALongRunTaskCanBeAParent(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "子", "ふつう")
	_, err := s.SetLong(id["親"], true, task.ActorHuman)
	require.NoError(t, err)

	child, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, id["親"], child.Parent)

	_, err = s.SetParent(id["子"], id["ふつう"], task.ActorHuman)
	require.Error(t, err, "a queue task is not a parent")
	_, err = s.SetParent(id["親"], id["親"], task.ActorHuman)
	require.Error(t, err, "not its own parent")
	_, err = s.SetParent(id["子"], "T999", task.ActorHuman)
	require.ErrorIs(t, err, task.ErrNotFound)

	cleared, err := s.SetParent(id["子"], "", task.ActorHuman)
	require.NoError(t, err)
	assert.Empty(t, cleared.Parent)
}

func TestSetParent_OneLevelOnly(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "子")
	_, err := s.SetLong(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetLong(id["子"], true, task.ActorHuman)
	require.Error(t, err, "a child cannot become long-run")
}

func TestUndo_LongRunPutsTheTaskBackWhereItWas(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C")
	_, err := s.SetFocus(id["B"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetLong(id["B"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	b, err := s.Get(id["B"])
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, b.State)
	assert.True(t, b.Focused, "the focus mark comes back too")
	assert.Equal(t, []string{"B", "A", "C"}, queueNames(t, s))
}

func TestUndo_UnlongAndDoneReturnToTheLongRunBand(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B")
	_, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetLong(id["A"], false, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	a, _ := s.Get(id["A"])
	assert.Equal(t, task.StateLong, a.State)

	_, err = s.Done(id["A"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	a, _ = s.Get(id["A"])
	assert.Equal(t, task.StateLong, a.State, "finished from the band, undone back to it")
	assert.Equal(t, []string{"B"}, queueNames(t, s))
}

// Adding a child is one change: a parent that is not long-run refuses it, and
// one undo takes the child back out.
func TestAddChild_IsOneChange(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "ふつう")
	_, err := s.AddChild("子", "", "", id["ふつう"], nil, "", "", task.ActorHuman)
	require.Error(t, err)
	tasks, _ := s.List()
	assert.Len(t, tasks, 2, "nothing is added without its parent")

	_, err = s.SetLong(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	kid, err := s.AddChild("子", "", "", id["親"], nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, id["親"], kid.Parent)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Get(kid.ID)
	require.ErrorIs(t, err, task.ErrNotFound, "one undo removes the child")
}

func TestUndo_ParentSet(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "子")
	_, err := s.SetLong(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	c, _ := s.Get(id["子"])
	assert.Empty(t, c.Parent)
}

// Long-run tasks sit after the queue in Order, so a reload keeps the queue
// numbered from 0 and the long-run task out of it.
func TestLongRun_SurvivesAReload(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)
	id := addAll(t, s, "A", "B")
	_, err := s.SetLong(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	re := openAt(t, dir)
	a, err := re.Get(id["A"])
	require.NoError(t, err)
	assert.Equal(t, task.StateLong, a.State)
	assert.Equal(t, []string{"B"}, queueNames(t, re))
}

func openAt(t *testing.T, dir string) *task.Store {
	t.Helper()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)
	return s
}
