package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskapi"
)

func TestSplitIDs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"spaces", []string{"T1", "T2"}, []string{"T1", "T2"}},
		{"commas", []string{"T1,T2,T3"}, []string{"T1", "T2", "T3"}},
		{"mixed", []string{"T1,T2", "T3"}, []string{"T1", "T2", "T3"}},
		{"empty parts", []string{"T1,,T2,", ",T3"}, []string{"T1", "T2", "T3"}},
		{"spaces around commas", []string{"T1, T2"}, []string{"T1", "T2"}},
		{"nothing but commas", []string{",,"}, nil},
		{"repeats keep the first", []string{"T1,T1,T2", "T1"}, []string{"T1", "T2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, splitIDs(c.args))
		})
	}
}

func newRemoveServer(t *testing.T) (*task.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	r := gin.New()
	taskapi.New(store, time.Local).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return store, srv.URL
}

func TestRemoveCommand_AcceptsCommaSeparatedIDs(t *testing.T) {
	store, url := newRemoveServer(t)
	var ids []string
	for _, name := range []string{"a", "b", "c", "keep"} {
		added, err := store.Add(name, "", "", task.ActorHuman)
		require.NoError(t, err)
		ids = append(ids, added.ID)
	}

	out, err := runTodo(t, url, "rm", ids[0]+","+ids[1], ids[2])
	require.NoError(t, err)

	assert.Contains(t, out, "削除 "+ids[0])
	assert.Contains(t, out, "削除 "+ids[1])
	assert.Contains(t, out, "削除 "+ids[2])
	assert.Less(t, indexOf(out, ids[0]), indexOf(out, ids[1]), "removed in the order given")
	assert.Less(t, indexOf(out, ids[1]), indexOf(out, ids[2]), "removed in the order given")
	_, err = store.Get(ids[3])
	assert.NoError(t, err, "the task not named is kept")
	for _, id := range ids[:3] {
		_, err := store.Get(id)
		assert.Error(t, err, id+" is removed")
	}
}

func TestRemoveCommand_StopsAtFirstFailureInCommaList(t *testing.T) {
	store, url := newRemoveServer(t)
	a, err := store.Add("a", "", "", task.ActorHuman)
	require.NoError(t, err)
	b, err := store.Add("b", "", "", task.ActorHuman)
	require.NoError(t, err)

	assert.Error(t, failTodo(t, url, "rm", a.ID+",T999,"+b.ID))

	_, err = store.Get(a.ID)
	assert.Error(t, err, "the one before the typo is removed")
	_, err = store.Get(b.ID)
	assert.NoError(t, err, "the one after the typo is kept")
}

func TestRemoveCommand_RejectsOnlyCommas(t *testing.T) {
	_, url := newRemoveServer(t)
	assert.Error(t, failTodo(t, url, "rm", ","))
}
