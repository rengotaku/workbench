package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A mistyped subcommand used to become a task: `todo show T094` added
// "show T094" (#97).
func TestAdd_UnknownSubcommandAddsNothing(t *testing.T) {
	store, url := newRemoveServer(t)

	err := failTodo(t, url, "show", "T094")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"show" というサブコマンドはありません`)
	assert.Contains(t, err.Error(), "todo -- 'show T094'")

	tasks, _ := store.List()
	assert.Empty(t, tasks)
}

func TestAdd_WaysToAddKeepWorking(t *testing.T) {
	store, url := newRemoveServer(t)

	for _, args := range [][]string{
		{"牛乳", "買う"},
		{"show T094"},
		{"--", "show", "T094"},
		{"資料 [1]", "--link", "https://example.com/a 一言"},
	} {
		out, err := runTodo(t, url, args...)
		require.NoError(t, err, args)
		assert.Contains(t, out, "追加 ")
	}

	tasks, _ := store.List()
	names := make([]string, 0, len(tasks))
	for _, tk := range tasks {
		names = append(names, tk.Name)
	}
	assert.ElementsMatch(t, []string{"牛乳 買う", "show T094", "show T094", "資料 [1]"}, names)
}
