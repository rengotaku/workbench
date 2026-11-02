package taskclient_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oneTask = `{"task":{"id":"T1","name":"x","state":"long"}}`

func TestLong_PostsOnToTheLongEndpoint(t *testing.T) {
	c, rec := serve(t, http.StatusOK, oneTask)
	_, _, err := c.Long("T1", true)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, rec.method)
	assert.Equal(t, "/api/tasks/T1/long", rec.path)
	assert.Equal(t, true, rec.body["on"])
}

func TestSetParent_PatchesTheParent(t *testing.T) {
	c, rec := serve(t, http.StatusOK, oneTask)
	_, _, err := c.SetParent("T2", "T1")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPatch, rec.method)
	assert.Equal(t, "/api/tasks/T2", rec.path)
	assert.Equal(t, "T1", rec.body["parent"])
}

func TestAddChild_SendsTheParent(t *testing.T) {
	c, rec := serve(t, http.StatusOK, oneTask)
	_, _, err := c.AddChild("T1", "子", "", "")
	require.NoError(t, err)
	assert.Equal(t, "/api/tasks", rec.path)
	assert.Equal(t, "T1", rec.body["parent"])
	assert.Equal(t, "子", rec.body["name"])
}

func TestList_ReadsTheLongBand(t *testing.T) {
	c, _ := serve(t, http.StatusOK,
		`{"long":[{"id":"T1","name":"親","state":"long","next":{"id":"T2","name":"子"},"kids_done":1,"kids_open":2}]}`)
	b, _, err := c.List("")
	require.NoError(t, err)
	require.Len(t, b.Long, 1)
	require.NotNil(t, b.Long[0].Next)
	assert.Equal(t, "T2", b.Long[0].Next.ID)
	assert.Equal(t, 1, b.Long[0].KidsDone)
	assert.Equal(t, 2, b.Long[0].KidsOpen)
}

func TestSetTopic_PatchesTheTopic(t *testing.T) {
	c, rec := serve(t, http.StatusOK, oneTask)
	_, _, err := c.SetTopic("T1", "青環離")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPatch, rec.method)
	assert.Equal(t, "/api/tasks/T1", rec.path)
	assert.Equal(t, "青環離", rec.body["topic"])
}
