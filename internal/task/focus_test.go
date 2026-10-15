package task_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// queueNames lists the open tasks by name, in queue order.
func queueNames(t *testing.T, s *task.Store) []string {
	t.Helper()
	tasks, _ := s.List()
	open := make([]task.Task, 0, len(tasks))
	for _, x := range tasks {
		if x.State == task.StateOpen {
			open = append(open, x)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].Order < open[j].Order })
	names := make([]string, len(open))
	for i, x := range open {
		names[i] = x.Name
	}
	return names
}

// addAll adds tasks named by the arguments, in order, and returns their IDs by name.
func addAll(t *testing.T, s *task.Store, names ...string) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, n := range names {
		added, err := s.Add(n, "", "", task.ActorHuman)
		require.NoError(t, err)
		ids[n] = added.ID
	}
	return ids
}

// Focus is the top of the queue (ADR todo-0012): focusing moves a task to the
// end of the focused block, and unfocusing puts it just below that block.
func TestSetFocus_MovesTheTaskToTheEndOfTheFocusedBlock(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C", "D")

	_, err := s.SetFocus(id["C"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"C", "A", "B", "D"}, queueNames(t, s))

	_, err = s.SetFocus(id["D"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"C", "D", "A", "B"}, queueNames(t, s))

	off, err := s.SetFocus(id["C"], false, task.ActorHuman)
	require.NoError(t, err)
	assert.False(t, off.Focused)
	assert.Equal(t, []string{"D", "C", "A", "B"}, queueNames(t, s), "it lands just below the focused block")
}

// A drop names the place: the position is kept, clamped to the block the task
// belongs to after the change.
func TestFocusAt_PutsTheTaskWhereItWasDropped(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C", "D")
	_, err := s.SetFocus(id["A"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(id["B"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.FocusAt(id["D"], true, 1, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "D", "B", "C"}, queueNames(t, s))

	_, err = s.FocusAt(id["A"], false, 3, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"D", "B", "C", "A"}, queueNames(t, s))

	// A position inside the other block is clamped to the edge of its own.
	_, err = s.FocusAt(id["C"], true, 3, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"D", "B", "C", "A"}, queueNames(t, s))
	_, err = s.FocusAt(id["D"], false, 0, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"B", "C", "D", "A"}, queueNames(t, s))
}

// Reordering cannot break the block: a focused task stays among the focused,
// an unfocused one stays below them.
func TestReorder_KeepsTheFocusedBlockOnTop(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C")
	_, err := s.SetFocus(id["A"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Reorder(id["C"], 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "C", "B"}, queueNames(t, s))

	_, err = s.Reorder(id["A"], 2, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "C", "B"}, queueNames(t, s))
}

// A focused task that comes back from waiting returns to the focused block,
// not to the end of the queue.
func TestResume_PutsAFocusedTaskBackInTheBlock(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C")
	_, err := s.SetFocus(id["C"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Wait(id["C"], "相手", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Resume(id["C"], "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"C", "A", "B"}, queueNames(t, s))
}

// Undo puts the task back where it was as well as unmarking it.
func TestUndo_ReversesFocusAndItsMove(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C")
	_, err := s.SetFocus(id["C"], true, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(id["C"])
	require.NoError(t, err)
	assert.False(t, got.Focused)
	assert.Equal(t, []string{"A", "B", "C"}, queueNames(t, s))
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

// A drop across the band carries the revision it was read at, the same as a
// reorder: a list that moved in between is refused, not changed elsewhere.
func TestFocusAt_RefusesAStaleRevision(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C")
	read := s.Revision()
	_, err := s.Add("D", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.FocusAt(id["C"], true, 0, read, task.ActorHuman)
	require.ErrorIs(t, err, task.ErrStale)

	got, err := s.Get(id["C"])
	require.NoError(t, err)
	assert.False(t, got.Focused, "nothing changes on a refused drop")
}
