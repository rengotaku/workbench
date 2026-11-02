package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskclient"
)

// captureStdout runs fn and returns everything it printed. printBoard writes
// straight to os.Stdout, and there is no existing test harness in this
// package to route through, so the output is captured at the file descriptor
// the same way `todo list` itself is checked by hand.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdout
	os.Stdout = w
	fn()
	require.NoError(t, w.Close())
	os.Stdout = orig

	buf := make([]byte, 64*1024)
	n, err := r.Read(buf)
	if err != nil && n == 0 {
		require.NoError(t, err)
	}
	require.NoError(t, r.Close())
	return string(buf[:n])
}

// 13. `todo list` shows a [いつか] section with arrived items first, and
// marks them.
func TestPrintBoard_ShowsSomedaySectionWithArrivedFirst(t *testing.T) {
	board := taskclient.Board{
		Today: "2026-09-18",
		Someday: []taskclient.Task{
			{Task: task.Task{ID: "T010", Name: "到来した", Until: "2026-09-01"}, UntilReached: true},
			{Task: task.Task{ID: "T011", Name: "まだ先", Until: "2026-10-20"}, UntilReached: false},
		},
	}

	out := captureStdout(t, func() { printBoard(board) })

	assert.Contains(t, out, "[いつか]")
	iSection := indexOf(out, "[いつか]")
	iArrived := indexOf(out, "T010")
	iFuture := indexOf(out, "T011")
	require.NotEqual(t, -1, iArrived)
	require.NotEqual(t, -1, iFuture)
	assert.Greater(t, iArrived, iSection, "the arrived row sits under the [いつか] heading")
	assert.Less(t, iArrived, iFuture, "the arrived task is listed before the one still in the future")
	assert.Contains(t, out, "●", "the arrived row is marked")
}

func TestPrintBoard_HidesSomedaySectionWhenEmpty(t *testing.T) {
	out := captureStdout(t, func() { printBoard(taskclient.Board{Today: "2026-09-18"}) })
	assert.NotContains(t, out, "[いつか]")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestDueSuffix_KeepsTheDateBesideTheCoarseLabel(t *testing.T) {
	assert.Equal(t, "  [2026-12-07 あと1か月]", dueSuffix(taskclient.Task{Task: task.Task{Due: "2026-12-07"}, DueLabel: "あと1か月"}))
	assert.Equal(t, "  [2026-10-30]", dueSuffix(taskclient.Task{Task: task.Task{Due: "2026-10-30"}}))
	assert.Equal(t, "  [2026-11-04 から]", dueSuffix(taskclient.Task{Task: task.Task{Due: "2026-11-04"}, DueLabel: "11/4 から"}))
	assert.Equal(t, "", dueSuffix(taskclient.Task{}))
}

// Rows left alone for a while say so; rows with nothing to show stay as they were.
func TestPrintBoard_ShowsIdleDays(t *testing.T) {
	idle := taskclient.Task{Task: task.Task{ID: "T001", Name: "止まり"}, IdleDays: 5}
	calm := taskclient.Task{Task: task.Task{ID: "T002", Name: "ふつう"}}
	out := captureStdout(t, func() {
		printBoard(taskclient.Board{Queue: []taskclient.Task{idle, calm}})
	})
	assert.Contains(t, out, "止まり  止まって 5日")
	assert.NotContains(t, out, "ふつう  止まって")
}

// The long section sits below the queue, with the review date and why it needs
// a review, and a one-line count goes above the queue.
func TestPrintBoard_LongSectionAndReviewCount(t *testing.T) {
	needs := taskclient.LongTask{Task: taskclient.Task{Task: task.Task{ID: "T051", Name: "切替", ReviewOn: "2026-10-20"}, IdleDays: 9},
		NeedsReview: true, ReviewReasons: []string{"no_next", "review_due", "stalled"}}
	calm := taskclient.LongTask{Task: taskclient.Task{Task: task.Task{ID: "T052", Name: "平穏", ReviewOn: "2026-11-04"}}, KidsOpen: 1}
	queue := taskclient.Task{Task: task.Task{ID: "T001", Name: "ふつう"}}
	out := captureStdout(t, func() {
		printBoard(taskclient.Board{Queue: []taskclient.Task{queue}, Long: []taskclient.LongTask{needs, calm}})
	})
	assert.Contains(t, out, "長期の見直し 1 件")
	assert.Less(t, strings.Index(out, "長期の見直し 1 件"), strings.Index(out, "[着手順]"))
	assert.Less(t, strings.Index(out, "[着手順]"), strings.Index(out, "[長期]"))
	assert.Contains(t, out, "見る 10/20")
	assert.Contains(t, out, "要見直し: 次の一手なし・見直し日 10/20 を過ぎた・止まって 9日")
	assert.Contains(t, out, "見る 11/4")
	assert.NotContains(t, out, "T052  平穏  子 完了 0 / 未完 1  見る 11/4  要見直し")
}

func TestReviewDate(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	for arg, want := range map[string]string{
		"+1w": "2026-11-04", "+2w": "2026-11-11", "+1m": "2026-11-28", "+3d": "2026-10-31", "2026-12-01": "2026-12-01",
	} {
		got, err := reviewDate(arg, now)
		require.NoError(t, err, arg)
		assert.Equal(t, want, got, arg)
	}
	_, err := reviewDate("tomorrow", now)
	require.Error(t, err)
}
