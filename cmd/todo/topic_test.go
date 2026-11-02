package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskclient"
)

func TestTopicCommand_SetsAndClears(t *testing.T) {
	store, url := newRemoveServer(t)
	a, err := store.Add("名前", "", "", task.ActorHuman)
	require.NoError(t, err)

	out, err := runTodo(t, url, "topic", a.ID, "青環離")
	require.NoError(t, err)
	assert.Contains(t, out, "{青環離}")
	got, _ := store.Get(a.ID)
	assert.Equal(t, "青環離", got.Topic)

	out, err = runTodo(t, url, "topic", a.ID, "-")
	require.NoError(t, err)
	assert.Contains(t, out, "話題を外した")
	got, _ = store.Get(a.ID)
	assert.Empty(t, got.Topic)
}

func TestTopicCommand_TooLongIsAnError(t *testing.T) {
	store, url := newRemoveServer(t)
	a, err := store.Add("名前", "", "", task.ActorHuman)
	require.NoError(t, err)
	err = failTodo(t, url, "topic", a.ID, strings.Repeat("あ", task.MaxTopicLen+1))
	require.Error(t, err)
}

func TestAdd_WithTopicFlag(t *testing.T) {
	store, url := newRemoveServer(t)
	_, err := runTodo(t, url, "資料", "--topic", " FUNDINNO ")
	require.NoError(t, err)
	tasks, _ := store.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, "FUNDINNO", tasks[0].Topic)
}

func TestAdd_BadTopicAddsNothing(t *testing.T) {
	store, url := newRemoveServer(t)
	err := failTodo(t, url, "資料", "--topic", strings.Repeat("あ", task.MaxTopicLen+1))
	require.Error(t, err)
	tasks, _ := store.List()
	assert.Empty(t, tasks)
}

func TestPrintBoard_ShowsTopicInBraces(t *testing.T) {
	board := taskclient.Board{
		Today: "2026-11-02",
		Focus: []taskclient.Task{{Task: task.Task{ID: "T001", Name: "帯", Topic: "学習"}}},
		Queue: []taskclient.Task{
			{Task: task.Task{ID: "T002", Name: "あり", Topic: "青環離"}},
			{Task: task.Task{ID: "T003", Name: "なし"}},
		},
	}
	out := captureStdout(t, func() { printBoard(board) })
	assert.Contains(t, out, "あり  {青環離}")
	assert.Contains(t, out, "帯  {学習}")
	assert.NotContains(t, out, "なし  {")
	assert.NotContains(t, out, "#青環離")
}
