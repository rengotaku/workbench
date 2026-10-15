package task_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// 1. shelving an open task takes it out of the queue and gives it an until.
func TestShelve_MovesAnOpenTaskToSomeday(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("あとで見る", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(added.ID, true, task.ActorHuman)
	require.NoError(t, err)

	shelved, err := s.Shelve(added.ID, dateIn(30), task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateSomeday, shelved.State)
	assert.Equal(t, dateIn(30), shelved.Until)
	assert.False(t, shelved.Focused, "shelving takes it out of the focus band")

	tasks, _ := s.List()
	for _, tk := range tasks {
		assert.NotEqual(t, task.StateOpen, tk.State, "nothing else is open, so the queue is empty")
	}
}

// 2. omitting --until sets it a month out, from a fixed reference so the
// comparison cannot flake.
func TestShelve_WithoutAnUntilDefaultsToOneMonthOut(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("いつか", "", "", task.ActorHuman)
	require.NoError(t, err)

	expect := today().AddDate(0, 1, 0).Format(task.DateLayout)
	shelved, err := s.Shelve(added.ID, "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, expect, shelved.Until)
}

// 3. a waiting task can be shelved too — dreaming of resuming it later is a
// legitimate reason to stop chasing someone for now.
func TestShelve_AWaitingTaskCanBeShelved(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("相手待ちだが寝かせる", "", "", task.ActorHuman)
	_, err := s.Wait(added.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)

	shelved, err := s.Shelve(added.ID, dateIn(10), task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateSomeday, shelved.State)
}

// 4. Order is renumbered open → waiting → someday → done.
func TestShelve_OrderRunsOpenWaitingSomedayDone(t *testing.T) {
	s := newStore(t)
	open, _ := s.Add("着手順", "", "", task.ActorHuman)
	waiting, _ := s.Add("待ち", "", "", task.ActorHuman)
	someday, _ := s.Add("いつか", "", "", task.ActorHuman)
	done, _ := s.Add("完了", "", "", task.ActorHuman)

	_, err := s.Wait(waiting.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Shelve(someday.ID, "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Done(done.ID, task.ActorHuman)
	require.NoError(t, err)

	order := map[string]int{}
	tasks, _ := s.List()
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[open.ID])
	assert.Equal(t, 1, order[waiting.ID])
	assert.Equal(t, 2, order[someday.ID])
	assert.Equal(t, 3, order[done.ID])
}

// 5. resuming a someday task reopens it at the end of the queue and clears
// Until.
func TestResume_ASomedayTaskGoesToTheEndOfTheQueueAndClearsUntil(t *testing.T) {
	s := newStore(t)
	first, _ := s.Add("先にある", "", "", task.ActorHuman)
	shelved, _ := s.Add("棚上げする", "", "", task.ActorHuman)
	_, err := s.Shelve(shelved.ID, dateIn(5), task.ActorHuman)
	require.NoError(t, err)
	second, _ := s.Add("あとから足した", "", "", task.ActorHuman)

	back, err := s.Resume(shelved.ID, "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, back.State)
	assert.Empty(t, back.Until)

	order := map[string]int{}
	tasks, _ := s.List()
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[first.ID])
	assert.Equal(t, 1, order[second.ID])
	assert.Equal(t, 2, order[shelved.ID], "it comes back at the end, not where it was")
}

// 6. someday → done is allowed: it can get finished while shelved.
func TestDone_ASomedayTaskCanBeCompleted(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("棚上げ中に片付いた", "", "", task.ActorHuman)
	_, err := s.Shelve(added.ID, "", task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateDone, done.State)
}

// 7. undoing a shelve restores state, order, until and focused.
func TestUndo_ReversesAShelve(t *testing.T) {
	s := newStore(t)
	other, _ := s.Add("他のタスク", "", "", task.ActorHuman)
	added, _ := s.Add("棚上げする", "", "", task.ActorHuman)
	_, err := s.SetFocus(added.ID, true, task.ActorHuman)
	require.NoError(t, err)
	beforeOrder := 0 // focusing moved it to the top (ADR todo-0012): 0 = added, 1 = other

	_, err = s.Shelve(added.ID, dateIn(30), task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, got.State)
	assert.Empty(t, got.Until)
	assert.True(t, got.Focused, "focused is put back the way it was")
	assert.Equal(t, beforeOrder, got.Order)

	otherAfter, err := s.Get(other.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, otherAfter.Order)
}

// 8. a shelve event is not asked about — someday is out of scope for reasons.
func TestShelve_NeedsReasonIsFalse(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("棚上げ", "", "", task.ActorHuman)
	_, err := s.Shelve(added.ID, "", task.ActorHuman)
	require.NoError(t, err)

	events, err := s.Events(added.ID)
	require.NoError(t, err)
	var shelved task.Event
	for _, e := range events {
		if e.Kind == task.KindShelved {
			shelved = e
		}
	}
	require.Equal(t, task.KindShelved, shelved.Kind)
	assert.False(t, shelved.NeedsReason(1))

	pending, err := s.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending, "shelving is not worth asking about")
}

// 10. shelving a someday task again just moves its until — the state stays
// someday, so a push-back does not take a lap through the queue.
func TestShelve_ASomedayTaskWithAnExplicitUntilExtendsInPlace(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("いつか延ばす", "", "", task.ActorHuman)
	_, err := s.Shelve(added.ID, dateIn(10), task.ActorHuman)
	require.NoError(t, err)

	extended, err := s.Shelve(added.ID, dateIn(40), task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateSomeday, extended.State)
	assert.Equal(t, dateIn(40), extended.Until)
}

// 11. omitting --until on an already-someday task extends it to a month from
// today, the same default as a fresh shelve.
func TestShelve_ASomedayTaskWithoutUntilExtendsByOneMonthFromToday(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("いつか延ばす（既定）", "", "", task.ActorHuman)
	_, err := s.Shelve(added.ID, dateIn(3), task.ActorHuman)
	require.NoError(t, err)

	expect := today().AddDate(0, 1, 0).Format(task.DateLayout)
	extended, err := s.Shelve(added.ID, "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, expect, extended.Until)
}

// 12. extending a someday task does not touch Order — it never leaves the
// someday band, so open → waiting → someday → done stays intact.
func TestShelve_ExtendingASomedayTaskDoesNotChangeOrder(t *testing.T) {
	s := newStore(t)
	open, _ := s.Add("着手順", "", "", task.ActorHuman)
	waiting, _ := s.Add("待ち", "", "", task.ActorHuman)
	someday, _ := s.Add("いつか", "", "", task.ActorHuman)
	done, _ := s.Add("完了", "", "", task.ActorHuman)

	_, err := s.Wait(waiting.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Shelve(someday.ID, dateIn(10), task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Done(done.ID, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Shelve(someday.ID, dateIn(50), task.ActorHuman)
	require.NoError(t, err)

	order := map[string]int{}
	tasks, _ := s.List()
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[open.ID])
	assert.Equal(t, 1, order[waiting.ID])
	assert.Equal(t, 2, order[someday.ID])
	assert.Equal(t, 3, order[done.ID])
}

// 13. undoing an extension puts the until back where it was, without leaving
// someday — that is not what was undone.
func TestUndo_ReversesAShelveExtensionWithoutLeavingSomeday(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("いつか延ばす", "", "", task.ActorHuman)
	_, err := s.Shelve(added.ID, dateIn(10), task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Shelve(added.ID, dateIn(40), task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateSomeday, got.State, "undoing the extension stays someday")
	assert.Equal(t, dateIn(10), got.Until, "until goes back to what it was before the extension")
}

// 14. a done task still cannot be shelved — someday only reaches from open or
// waiting, in place or extending someday itself.
func TestShelve_ADoneTaskCannotBeShelved(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("完了済み", "", "", task.ActorHuman)
	_, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Shelve(added.ID, dateIn(10), task.ActorHuman)
	require.Error(t, err)
}

// 9. the waiting 14-day rule does not pick up a someday task that used to be
// waiting.
func TestAsk_DoesNotStallASomedayTaskThatWasOnceWaiting(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	added, _ := s.Add("長く放置される", "", "", task.ActorHuman)
	_, err = s.Wait(added.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Shelve(added.ID, dateIn(60), task.ActorHuman)
	require.NoError(t, err)

	require.NoError(t, backdateHandoff(dir, added.ID, task.AskThresholdDays+1))
	_ = s.Close()
	reloaded, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	pending, err := reloaded.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending, "a shelved task is not waiting, so the stall rule does not see it")
}
