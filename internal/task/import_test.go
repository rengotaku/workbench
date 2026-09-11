package task_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/todo-order/internal/task"
)

func TestImport_KeepsTheGivenOrderAndTheOldIDs(t *testing.T) {
	s := newStore(t)

	got, err := s.Import([]task.ImportItem{
		{Name: "DSC-11989 結論をコメント", Detail: "本人メモ"},
		{ID: "T004", Name: "話し方の改善", Due: dateIn(1)},
		{ID: "T002", Name: "終わっている作業", Done: true},
	}, task.ActorHuman)
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Auto-numbered rows start after the highest ID the old ledger had spent.
	assert.Equal(t, "T005", got[0].ID)
	assert.Equal(t, 0, got[0].Order, "the first item given is the top of the queue")
	assert.Equal(t, "本人メモ", got[0].Detail)

	assert.Equal(t, "T004", got[1].ID)
	assert.Equal(t, 1, got[1].Order)
	assert.Equal(t, dateIn(1), got[1].Due)

	// Completed rows arrive done and sit past the queue.
	done, err := s.Get("T002")
	require.NoError(t, err)
	assert.Equal(t, task.StateDone, done.State)
	require.NotNil(t, done.CompletedAt)

	// The next task added carries on from there rather than reusing a number.
	next, err := s.Add("移行後に足した", "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "T006", next.ID)
}

// Importing twice would duplicate every row, so the second run has to refuse.
func TestImport_RefusesAStoreThatAlreadyHoldsTasks(t *testing.T) {
	s := newStore(t)
	_, err := s.Add("既にある", "", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Import([]task.ImportItem{{Name: "移行分"}}, task.ActorHuman)
	assert.ErrorIs(t, err, task.ErrNotEmpty)
}

func TestImport_WritesNothingWhenARowIsBad(t *testing.T) {
	dir := t.TempDir()
	s, err := task.Open(dir, time.Local)
	require.NoError(t, err)

	_, err = s.Import([]task.ImportItem{
		{Name: "良い行"},
		{Name: "日付が壊れている", Due: "2026/09/12"},
	}, task.ActorHuman)
	require.Error(t, err)

	// Validation happens before any write, so the store is still empty.
	reopened, err := task.Open(dir, time.Local)
	require.NoError(t, err)
	assert.Empty(t, reopened.List(), "a bad row must leave the store untouched")
}

func TestImport_RefusesRowsItCannotTrust(t *testing.T) {
	cases := []struct {
		name  string
		want  string
		items []task.ImportItem
	}{
		{name: "nothing at all", items: nil, want: "nothing to import"},
		{name: "no name", items: []task.ImportItem{{Name: ""}}, want: "no name"},
		{name: "malformed ID", items: []task.ImportItem{{ID: "X1", Name: "名前"}}, want: "not T + digits"},
		{
			name:  "the same ID twice",
			items: []task.ImportItem{{ID: "T001", Name: "一つ目"}, {ID: "T001", Name: "二つ目"}},
			want:  "appears twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			_, err := s.Import(tc.items, task.ActorHuman)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestImport_RecordsAnEventPerRowSoTheTrailStartsFromTheMove(t *testing.T) {
	s := newStore(t)
	_, err := s.Import([]task.ImportItem{
		{ID: "T002", Name: "終わっている作業", Done: true},
		{ID: "T004", Name: "残っている作業"},
	}, task.ActorHuman)
	require.NoError(t, err)

	open, err := s.Events("T004")
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, task.KindCreated, open[0].Kind)

	closed, err := s.Events("T002")
	require.NoError(t, err)
	require.Len(t, closed, 2, "a completed row records both its arrival and its completion")
	assert.Equal(t, task.KindCreated, closed[0].Kind)
	assert.Equal(t, task.KindDone, closed[1].Kind)
}
