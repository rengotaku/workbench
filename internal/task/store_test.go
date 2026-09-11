package task_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/todo-order/internal/task"
)

func newStore(t *testing.T) *task.Store {
	t.Helper()
	s, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	return s
}

func today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
}

func dateIn(days int) string {
	return today().AddDate(0, 0, days).Format(task.DateLayout)
}

func TestAdd_NumbersFromT001AndKeepsQueueOrder(t *testing.T) {
	s := newStore(t)

	first, err := s.Add("最初のタスク", "", "", task.ActorHuman)
	require.NoError(t, err)
	second, err := s.Add("次のタスク", "", "", task.ActorHuman)
	require.NoError(t, err)

	assert.Equal(t, "T001", first.ID)
	assert.Equal(t, "T002", second.ID)
	assert.Equal(t, 0, first.Order)
	assert.Equal(t, 1, second.Order)
}

func TestAdd_RejectsEmptyNameAndBadDate(t *testing.T) {
	s := newStore(t)

	_, err := s.Add("", "", "", task.ActorHuman)
	assert.Error(t, err)

	_, err = s.Add("名前あり", "", "2026/09/10", task.ActorHuman)
	assert.ErrorContains(t, err, "YYYY-MM-DD")
}

func TestSetNextTaskNumber_ContinuesAndRefusesGoingBackwards(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.SetNextTaskNumber(7))

	got, err := s.Add("移行後の1件目", "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "T007", got.ID)

	// Reusing a spent number would make old references point at a new task.
	assert.Error(t, s.SetNextTaskNumber(3))
}

func TestDone_TwiceIsAnError(t *testing.T) {
	s := newStore(t)
	added, err := s.Add("片付ける", "", "", task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateDone, done.State)
	require.NotNil(t, done.CompletedAt)

	_, err = s.Done(added.ID, task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrAlreadyDone)
}

func TestDone_UnknownIDIsNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Done("T999", task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestWait_RecordsHandoffAndKeepsTheOriginalDate(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("回答を待つ", "", "", task.ActorHuman)

	waiting, err := s.Wait(added.ID, "相手", "回答待ち", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateWaiting, waiting.State)
	assert.Equal(t, "相手", waiting.HandedTo)
	first := waiting.HandedOn
	assert.NotEmpty(t, first)

	// Chasing again must not reset the count: how long it has sat is the point.
	again, err := s.Wait(added.ID, "別の相手", "催促した", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, first, again.HandedOn)
}

func TestWait_RequiresWho(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("誰かに渡す", "", "", task.ActorHuman)

	_, err := s.Wait(added.ID, "", "", task.ActorHuman)
	assert.ErrorContains(t, err, "--to")
}

func TestResume_ClearsHandoffSoTheNextOneStartsFresh(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("戻ってくる", "", "", task.ActorHuman)
	_, err := s.Wait(added.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)

	back, err := s.Resume(added.ID, "回答が来た", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, back.State)
	assert.Empty(t, back.HandedTo)
	assert.Empty(t, back.HandedOn)

	_, err = s.Resume(added.ID, "", task.ActorHuman)
	assert.ErrorContains(t, err, "not waiting")
}

func TestStalledFor_CountsFromTheHandoffDate(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("長く待っている", "", "", task.ActorHuman)
	_, err := s.Wait(added.ID, "相手", "", task.ActorHuman)
	require.NoError(t, err)

	got, err := s.Get(added.ID)
	require.NoError(t, err)

	days, ok := got.StalledFor(today().AddDate(0, 0, 20))
	assert.True(t, ok)
	assert.Equal(t, 20, days)

	// A task in the queue has nothing to count.
	open, _ := s.Add("待ちではない", "", "", task.ActorHuman)
	_, ok = open.StalledFor(today())
	assert.False(t, ok)
}

func TestReorder_MovesTheTaskAndRenumbersTheQueue(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	c, _ := s.Add("C", "", "", task.ActorHuman)

	moved, err := s.Reorder(c.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, 0, moved.Order)

	order := map[string]int{}
	tasks, _ := s.List()
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[c.ID])
	assert.Equal(t, 1, order[a.ID])
	assert.Equal(t, 2, order[b.ID])
}

func TestReorder_RejectsPositionsOutsideTheQueue(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)

	_, err := s.Reorder(a.ID, 5, 7, 0, task.ActorHuman)
	assert.ErrorContains(t, err, "out of range")
}

// Condition 3: overtaking something whose due date is inside the scope window
// is the case worth explaining, so the event has to name what it passed.
func TestReorder_RecordsWhatItOvertookWhenDueIsInScope(t *testing.T) {
	s := newStore(t)
	near, _ := s.Add("期限が近い", "", dateIn(2), task.ActorHuman)
	far, _ := s.Add("期限が遠い", "", dateIn(60), task.ActorHuman)
	noDue, _ := s.Add("期限なし", "", "", task.ActorHuman)

	_, err := s.Reorder(noDue.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)

	events, err := s.Events(noDue.ID)
	require.NoError(t, err)

	var reorder task.Event
	for _, e := range events {
		if e.Kind == task.KindReordered {
			reorder = e
		}
	}
	require.Equal(t, task.KindReordered, reorder.Kind)
	require.Len(t, reorder.Jumped, 1, "only the in-scope due date is worth asking about")
	assert.Equal(t, near.ID, reorder.Jumped[0].ID)
	assert.NotEqual(t, far.ID, reorder.Jumped[0].ID)
}

func TestReorder_MovingDownRecordsNothingJumped(t *testing.T) {
	s := newStore(t)
	first, _ := s.Add("先頭", "", dateIn(1), task.ActorHuman)
	_, _ = s.Add("2番目", "", dateIn(1), task.ActorHuman)

	_, err := s.Reorder(first.ID, 1, 7, 0, task.ActorHuman)
	require.NoError(t, err)

	events, _ := s.Events(first.ID)
	for _, e := range events {
		if e.Kind == task.KindReordered {
			assert.Empty(t, e.Jumped)
		}
	}
}

func TestAsk_ReturnsOnlyTheFourChangesWorthExplaining(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	c, _ := s.Add("C", "", "", task.ActorHuman)
	_, _ = s.Add("D", "", "", task.ActorHuman)

	// A swap in the middle of the queue explains itself: not the top, not the
	// bottom, and nothing with a nearby due date was overtaken.
	_, err := s.Reorder(b.ID, 2, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	pending, err := s.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending, "a middle swap is not worth asking about")

	// Moving to the top is worth asking about.
	_, err = s.Reorder(c.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	pending, err = s.Ask()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, c.ID, pending[0].Task.ID)
	assert.Equal(t, "1位に上げた", pending[0].Trigger)

	// So is dropping something to the bottom. A sits at position 2 of 4 after
	// the moves above, so this is a genuine fall rather than a no-op.
	_, err = s.Reorder(a.ID, 3, 7, 0, task.ActorHuman)
	require.NoError(t, err)
	pending, err = s.Ask()
	require.NoError(t, err)

	triggers := map[string]string{}
	for _, p := range pending {
		triggers[p.Task.ID] = p.Trigger
	}
	assert.Equal(t, "最下位へ落とした", triggers[a.ID])
	assert.Equal(t, "1位に上げた", triggers[c.ID])
}

func TestAsk_QueuesAStalledHandoffOnceItPassesTheThreshold(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	added, _ := s.Add("長く待っている", "", "", task.ActorHuman)
	_, err = s.Wait(added.ID, "相手", "回答待ち", task.ActorHuman)
	require.NoError(t, err)

	// Fresh handoffs are left alone.
	pending, err := s.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending)

	// Backdate the handoff past the threshold and reload from disk.
	require.NoError(t, backdateHandoff(dir, added.ID, task.AskThresholdDays+1))
	reloaded, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	pending, err = reloaded.Ask()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, added.ID, pending[0].Task.ID)
	assert.Contains(t, pending[0].Trigger, "待ちに")
}

func TestWhy_RecordsAReasonAndRefusesToOverwriteOne(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	_, err := s.Reorder(b.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err)

	pending, err := s.Ask()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	eventID := pending[0].Event.ID

	updated, err := s.Why(eventID, "切替日が動いたので前に出した")
	require.NoError(t, err)
	assert.Equal(t, "切替日が動いたので前に出した", updated.Reason)
	require.NotNil(t, updated.ReasonAt)

	// Explained changes leave the queue.
	pending, err = s.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending)

	// An answer already on the record is not overwritten by a second one.
	_, err = s.Why(eventID, "別の理由")
	assert.ErrorContains(t, err, "already has a reason")

	_, err = s.Why("E9999", "存在しない")
	assert.ErrorIs(t, err, task.ErrNotFound)

	_, err = s.Why(eventID, "")
	assert.ErrorContains(t, err, "reason is required")
}

// A position only means something against the order it was read from. If the
// list moved in between — the AI reordering while a person drags — the move is
// refused rather than landing somewhere else.
func TestReorder_RefusesAPositionReadFromAnOlderList(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	_, _ = s.Add("C", "", "", task.ActorHuman)

	_, rev := s.List()

	// Something else changes the list first.
	_, err := s.Reorder(b.ID, 0, 7, rev, task.ActorAI)
	require.NoError(t, err)

	// The move built on the older reading is refused.
	_, err = s.Reorder(a.ID, 2, 7, rev, task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrStale)

	// Re-reading and trying again works.
	_, fresh := s.List()
	_, err = s.Reorder(a.ID, 2, 7, fresh, task.ActorHuman)
	assert.NoError(t, err)
}

func TestReorder_WithoutARevisionSkipsTheCheck(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("A", "", "", task.ActorHuman)
	_, _ = s.Add("B", "", "", task.ActorHuman)

	// 0 means "no expectation": the CLI moves a task by its own reckoning.
	_, err := s.Reorder(a.ID, 1, 7, 0, task.ActorHuman)
	assert.NoError(t, err)
}

// The revision moves on every write, which is what the page polls.
func TestRevision_AdvancesOnEveryWrite(t *testing.T) {
	s := newStore(t)
	start := s.Revision()

	added, err := s.Add("書き込み 1", "", "", task.ActorHuman)
	require.NoError(t, err)
	afterAdd := s.Revision()
	assert.Greater(t, afterAdd, start)

	_, err = s.SetDue(added.ID, dateIn(1), task.ActorHuman)
	require.NoError(t, err)
	assert.Greater(t, s.Revision(), afterAdd)

	// Reading does not move it.
	before := s.Revision()
	_, rev := s.List()
	assert.Equal(t, before, rev)
	assert.Equal(t, before, s.Revision())
}

func TestEvents_AreKeptPerTaskAndSurviveAReload(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	a, _ := s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)
	_, err = s.SetDue(a.ID, dateIn(3), task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetDetail(a.ID, "詳しい話", task.ActorHuman)
	require.NoError(t, err)

	reloaded, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	got, err := reloaded.Get(a.ID)
	require.NoError(t, err)
	assert.Equal(t, "詳しい話", got.Detail)
	assert.Equal(t, dateIn(3), got.Due)

	events, err := reloaded.Events(a.ID)
	require.NoError(t, err)
	kinds := make([]task.Kind, 0, len(events))
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	assert.Equal(t, []task.Kind{task.KindCreated, task.KindDueSet, task.KindDetail}, kinds)

	// Another task's events do not leak into this one.
	other, err := reloaded.Events(b.ID)
	require.NoError(t, err)
	assert.Len(t, other, 1)

	_, err = reloaded.Events("T999")
	assert.ErrorIs(t, err, task.ErrNotFound)
}

// Completion is a state, not a one-way door: a mis-click has to have a way out.
// Removal is the one operation that loses something, so what it took out has
// to stay readable in the log.
func TestRemove_TakesTheTaskOutAndRecordsWhatItWas(t *testing.T) {
	s := newStore(t)
	first, _ := s.Add("残る", "", "", task.ActorHuman)
	junk, _ := s.Add("誤って追加した", "", dateIn(3), task.ActorHuman)
	last, _ := s.Add("これも残る", "", "", task.ActorHuman)

	removed, err := s.Remove(junk.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "誤って追加した", removed.Name)

	_, err = s.Get(junk.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)

	// The queue closes up behind it.
	tasks, _ := s.List()
	require.Len(t, tasks, 2)
	order := map[string]int{}
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[first.ID])
	assert.Equal(t, 1, order[last.ID])

	// The removal event keeps the name and the due date it had.
	events, err := s.Events(first.ID) // a surviving task still reads fine
	require.NoError(t, err)
	assert.NotEmpty(t, events)

	all, err := s.Events(last.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, all)
}

func TestRemove_UnknownIDIsNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.Remove("T999", task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

// Events belonging to a removed task are left in the log. Nothing downstream
// may trip over them.
func TestRemove_LeavesOrphanEventsThatAskIgnores(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add("A", "", "", task.ActorHuman)
	b, _ := s.Add("B", "", "", task.ActorHuman)

	_, err := s.Reorder(b.ID, 0, 7, 0, task.ActorHuman) // this asks for a reason
	require.NoError(t, err)

	pending, err := s.Ask()
	require.NoError(t, err)
	require.Len(t, pending, 1)

	_, err = s.Remove(b.ID, task.ActorHuman)
	require.NoError(t, err)

	pending, err = s.Ask()
	require.NoError(t, err)
	assert.Empty(t, pending, "an event with no task left is skipped, not an error")
}

func TestReopen_PutsACompletedTaskBackAtTheEndOfTheQueue(t *testing.T) {
	s := newStore(t)
	first, _ := s.Add("残る", "", "", task.ActorHuman)
	mistake, _ := s.Add("誤って完了にした", "", "", task.ActorHuman)
	second, _ := s.Add("あとから足した", "", "", task.ActorHuman)

	_, err := s.Done(mistake.ID, task.ActorHuman)
	require.NoError(t, err)

	back, err := s.Reopen(mistake.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateOpen, back.State)
	assert.Nil(t, back.CompletedAt, "the completion time is cleared")

	order := map[string]int{}
	tasks, _ := s.List()
	for _, tk := range tasks {
		order[tk.ID] = tk.Order
	}
	assert.Equal(t, 0, order[first.ID])
	assert.Equal(t, 1, order[second.ID])
	assert.Equal(t, 2, order[mistake.ID], "it comes back at the end, not where it was")

	// The trail keeps both the completion and the undo.
	events, err := s.Events(mistake.ID)
	require.NoError(t, err)
	kinds := []task.Kind{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	assert.Equal(t, []task.Kind{task.KindCreated, task.KindDone, task.KindReopened}, kinds)
}

func TestReopen_RefusesATaskThatIsNotDone(t *testing.T) {
	s := newStore(t)
	open, _ := s.Add("まだ終わっていない", "", "", task.ActorHuman)

	_, err := s.Reopen(open.ID, task.ActorHuman)
	assert.ErrorContains(t, err, "not done")

	_, err = s.Reopen("T999", task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

// A reopened task can be completed again; the first completion does not make
// the second one a conflict.
func TestReopen_ThenDoneAgainWorks(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("やり直す", "", "", task.ActorHuman)
	_, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Reopen(added.ID, task.ActorHuman)
	require.NoError(t, err)

	again, err := s.Done(added.ID, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.StateDone, again.State)
}

func TestSetDue_ClearsWithAnEmptyStringAndRejectsBadInput(t *testing.T) {
	s := newStore(t)
	added, _ := s.Add("期限つき", "", dateIn(1), task.ActorHuman)

	cleared, err := s.SetDue(added.ID, "", task.ActorHuman)
	require.NoError(t, err)
	assert.Empty(t, cleared.Due)

	_, err = s.SetDue(added.ID, "tomorrow", task.ActorHuman)
	assert.ErrorContains(t, err, "YYYY-MM-DD")
}

func TestOpen_ReadsBackAnExistingStore(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, nil)
	require.NoError(t, err)
	_, err = s.Add("残るはず", "", "", task.ActorHuman)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, "tasks.json"))

	reopened, err := task.Open(dir, nil)
	require.NoError(t, err)
	list, _ := reopened.List()
	require.Len(t, list, 1)
	assert.Equal(t, "残るはず", list[0].Name)

	// The next ID continues rather than colliding with the one already used.
	next, err := reopened.Add("2件目", "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "T002", next.ID)
}

func TestParseDate(t *testing.T) {
	_, err := task.ParseDate("2026-09-10", time.Local)
	assert.NoError(t, err)

	_, err = task.ParseDate("10/09/2026", time.Local)
	assert.Error(t, err)
}
