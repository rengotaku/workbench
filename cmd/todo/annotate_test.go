package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
)

func runTodo(t *testing.T, url string, args ...string) (string, error) {
	t.Helper()
	origServer, origJSON := serverURL, asJSON
	t.Cleanup(func() { serverURL, asJSON = origServer, origJSON })
	asJSON = false
	cmd := rootCmd()
	cmd.SetArgs(append([]string{"--server", url}, args...))
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	return out, err
}

// failTodo runs a command that is expected to fail before printing anything,
// so there is no stdout to capture.
func failTodo(t *testing.T, url string, args ...string) error {
	t.Helper()
	origServer, origJSON := serverURL, asJSON
	t.Cleanup(func() { serverURL, asJSON = origServer, origJSON })
	asJSON = false
	cmd := rootCmd()
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"--server", url}, args...))
	return cmd.Execute()
}

func TestAnnotateCommand_WritesThroughTheAPIAsTheActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	r := gin.New()
	taskapi.New(store, time.Local).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	added, err := store.Add("調べる", "", "", task.ActorHuman)
	require.NoError(t, err)

	t.Setenv("TODO_ORDER_ACTOR", "ai")
	out, err := runTodo(t, srv.URL, "annotate", added.ID, "--kind", "quote", "--body", "引用です", "--source", "slack")
	require.NoError(t, err)
	assert.Contains(t, out, "補足 "+added.ID)

	file := filepath.Join(t.TempDir(), "b.md")
	require.NoError(t, os.WriteFile(file, []byte("ファイルから"), 0o600))
	_, err = runTodo(t, srv.URL, "annotate", added.ID, "--kind", "analysis", "--body-file", file)
	require.NoError(t, err)

	got, err := store.Get(added.ID)
	require.NoError(t, err)
	require.Len(t, got.Annotations, 2)
	assert.Equal(t, task.ActorAI, got.Annotations[0].Author)
	assert.Equal(t, "slack", got.Annotations[0].Source)
	assert.Equal(t, "ファイルから", got.Annotations[1].Body)

	for _, args := range [][]string{
		{"annotate", added.ID, "--body", "x"},
		{"annotate", added.ID, "--kind", "memo"},
		{"annotate", added.ID, "--kind", "memo", "--body", "x", "--body-file", file},
		{"annotate", added.ID, "--kind", "bogus", "--body", "x"},
		{"annotate", "T999", "--kind", "memo", "--body", "x"},
	} {
		assert.Error(t, failTodo(t, srv.URL, args...), args)
	}
	got, _ = store.Get(added.ID)
	assert.Len(t, got.Annotations, 2)

	root := rootCmd()
	assert.Contains(t, root.UsageString(), "annotate")
	assert.Contains(t, annotateCmd().Long, "undo")
}
