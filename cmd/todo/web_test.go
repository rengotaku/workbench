package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddPageURL(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8085/#add", addPageURL("http://127.0.0.1:8085"))
	assert.Equal(t, "http://127.0.0.1:8085/#add", addPageURL("http://127.0.0.1:8085/"))
}

func TestOpenAddPage_OpensServerURL(t *testing.T) {
	orig, origServer := openBrowser, serverURL
	t.Cleanup(func() { openBrowser, serverURL = orig, origServer })

	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	serverURL = "http://127.0.0.1:18085"

	require.NoError(t, openAddPage())
	assert.Equal(t, "http://127.0.0.1:18085/#add", opened)
}

func TestOpenAddPage_ReportsOpenFailure(t *testing.T) {
	orig, origServer := openBrowser, serverURL
	t.Cleanup(func() { openBrowser, serverURL = orig, origServer })

	openBrowser = func(string) error { return errors.New("no browser") }
	serverURL = "http://127.0.0.1:18085"

	err := openAddPage()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http://127.0.0.1:18085/#add")
}

// Without a terminal (pipe, script, assistant) a bare `todo` must print help
// and never launch a browser.
func TestRootWithoutArgsFromPipePrintsHelp(t *testing.T) {
	orig := openBrowser
	t.Cleanup(func() { openBrowser = orig })
	called := false
	openBrowser = func(string) error { called = true; return nil }

	cmd := rootCmd()
	cmd.SetArgs([]string{})
	out := captureStdout(t, func() { require.NoError(t, cmd.Execute()) })

	assert.False(t, called)
	assert.Contains(t, out, "着手順で持つ TODO リスト")
}
