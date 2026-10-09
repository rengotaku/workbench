package main

import (
	"os"
	"testing"

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
