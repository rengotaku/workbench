package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var promptToday = time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local) // Wednesday

func TestPromptAdd_AsksEachField(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("Notion を確認\n水\nhttps://example.com/p\n")

	got, err := promptAdd(in, &out, promptToday)
	require.NoError(t, err)
	assert.Equal(t, addInput{Name: "Notion を確認", Due: "2026-10-07", Detail: "https://example.com/p"}, got)
	assert.Contains(t, out.String(), "タスク名")
	assert.Contains(t, out.String(), "期限")
	assert.Contains(t, out.String(), "詳細")
	assert.Contains(t, out.String(), "2026-10-07") // resolved date is echoed back
}

func TestPromptAdd_OptionalFieldsMayBeEmpty(t *testing.T) {
	var out bytes.Buffer
	got, err := promptAdd(strings.NewReader("やる\n\n\n"), &out, promptToday)
	require.NoError(t, err)
	assert.Equal(t, addInput{Name: "やる"}, got)
}

func TestPromptAdd_ReasksEmptyNameAndBadDue(t *testing.T) {
	var out bytes.Buffer
	got, err := promptAdd(strings.NewReader("\n  \nやる\nあした\n明日\n\n"), &out, promptToday)
	require.NoError(t, err)
	assert.Equal(t, addInput{Name: "やる", Due: "2026-10-01"}, got)
	assert.Equal(t, 3, strings.Count(out.String(), "タスク名"))
	assert.Contains(t, out.String(), "読めない期限")
}

func TestPromptAdd_EOFBeforeNameAborts(t *testing.T) {
	var out bytes.Buffer
	_, err := promptAdd(strings.NewReader(""), &out, promptToday)
	assert.Error(t, err)
}

func TestPromptAdd_EOFAfterNameKeepsWhatWasTyped(t *testing.T) {
	// A missing trailing newline (Ctrl-D after the last answer) must not
	// throw away the answers already given.
	var out bytes.Buffer
	got, err := promptAdd(strings.NewReader("やる\n金"), &out, promptToday)
	require.NoError(t, err)
	assert.Equal(t, addInput{Name: "やる", Due: "2026-10-02"}, got)
}
