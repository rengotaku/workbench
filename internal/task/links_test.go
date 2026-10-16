package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func TestAddWithLinks_NumbersFromOneInOrder(t *testing.T) {
	s := newStore(t)
	got, err := s.AddWithLinks("調べる [1] [2]", "", "", []task.LinkInput{
		{URL: " https://a.example/x ", Label: " 一つ目 "},
		{URL: "http://b.example/"},
	}, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []task.Link{
		{N: 1, URL: "https://a.example/x", Label: "一つ目"},
		{N: 2, URL: "http://b.example/"},
	}, got.Links)
}

func TestAddWithLinks_RejectsABadURLAndCreatesNothing(t *testing.T) {
	s := newStore(t)
	for _, bad := range []string{"", "ftp://a.example/", "javascript:alert(1)", "not a url", "https://"} {
		_, err := s.AddWithLinks("x", "", "", []task.LinkInput{{URL: "https://ok.example/"}, {URL: bad}}, task.ActorHuman)
		assert.Error(t, err, bad)
	}
	list, _ := s.List()
	assert.Empty(t, list)
}

func TestAddWithLinks_UndoRemovesTheTaskInOneStep(t *testing.T) {
	s := newStore(t)
	added, err := s.AddWithLinks("x [1]", "", "", []task.LinkInput{{URL: "https://a.example/"}}, task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindCreated, done.Kind)
	_, err = s.Get(added.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestSetLinks_ReplacesClearsAndSkipsNoOp(t *testing.T) {
	s := newStore(t)
	added, err := s.AddWithLinks("x", "", "", []task.LinkInput{{URL: "https://a.example/", Label: "a"}}, task.ActorHuman)
	require.NoError(t, err)

	same, err := s.SetLinks(added.ID, []task.LinkInput{{URL: "https://a.example/", Label: "a"}}, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, added.Links, same.Links)
	evs, err := s.Events(added.ID)
	require.NoError(t, err)
	assert.Len(t, evs, 1, "an unchanged list records nothing")

	got, err := s.SetLinks(added.ID, []task.LinkInput{{URL: "https://b.example/"}, {URL: "https://a.example/"}}, task.ActorAI)
	require.NoError(t, err)
	assert.Equal(t, []task.Link{{N: 1, URL: "https://b.example/"}, {N: 2, URL: "https://a.example/"}}, got.Links)

	_, err = s.SetLinks(added.ID, []task.LinkInput{{URL: "bad"}}, task.ActorHuman)
	require.Error(t, err)
	got, _ = s.Get(added.ID)
	assert.Len(t, got.Links, 2, "a rejected list leaves the links alone")

	got, err = s.SetLinks(added.ID, []task.LinkInput{}, task.ActorHuman)
	require.NoError(t, err)
	assert.Empty(t, got.Links)
}

func TestUndo_RestoresTheOldLinkListAndWalksBack(t *testing.T) {
	s := newStore(t)
	added, err := s.AddWithLinks("x", "", "", []task.LinkInput{{URL: "https://a.example/", Label: "a"}}, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetLinks(added.ID, []task.LinkInput{{URL: "https://b.example/"}}, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetLinks(added.ID, nil, task.ActorHuman)
	require.NoError(t, err)

	done, err := s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, task.KindLinks, done.Kind)
	got, _ := s.Get(added.ID)
	assert.Equal(t, []task.Link{{N: 1, URL: "https://b.example/"}}, got.Links)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	got, _ = s.Get(added.ID)
	assert.Equal(t, []task.Link{{N: 1, URL: "https://a.example/", Label: "a"}}, got.Links)
}

func TestLegacySingleURLKeepsWorkingBesideLinks(t *testing.T) {
	s := newStore(t)
	added, err := s.AddLinked("DSC-1 件", "", "", "https://old.example/", "DSC-1", task.ActorHuman)
	require.NoError(t, err)
	got, err := s.SetLinks(added.ID, []task.LinkInput{{URL: "https://new.example/"}}, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "https://old.example/", got.URL)
	assert.Equal(t, "DSC-1", got.LinkText)
	assert.Len(t, got.Links, 1)
}

func TestParseLinkInput(t *testing.T) {
	assert.Equal(t, task.LinkInput{URL: "https://a.example/"}, task.ParseLinkInput("  https://a.example/ "))
	assert.Equal(t, task.LinkInput{URL: "https://a.example/", Label: "仕様 書"}, task.ParseLinkInput("https://a.example/　仕様 書"))
}
