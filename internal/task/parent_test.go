package task_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// Any task that is not done may be a parent (ADR todo-0020).
func TestSetParent_OpenWaitingAndSomedayTasksCanBeParents(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "open", "wait", "some", "k1", "k2", "k3")
	_, err := s.Wait(id["wait"], "someone", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Shelve(id["some"], "", task.ActorHuman)
	require.NoError(t, err)

	for i, p := range []string{"open", "wait", "some"} {
		kid := []string{"k1", "k2", "k3"}[i]
		got, err := s.SetParent(id[kid], id[p], task.ActorHuman)
		require.NoError(t, err, p)
		assert.Equal(t, id[p], got.Parent)
	}
}

func TestSetParent_RejectsDoneSelfAndLoops(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "B", "C", "done")
	_, err := s.Done(id["done"], task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetParent(id["A"], id["done"], task.ActorHuman)
	require.Error(t, err, "a done task is not a parent")
	_, err = s.SetParent(id["A"], id["A"], task.ActorHuman)
	require.Error(t, err, "not its own parent")

	_, err = s.SetParent(id["B"], id["A"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["C"], id["B"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["A"], id["C"], task.ActorHuman)
	require.Error(t, err, "A -> B -> C -> A is a loop")
	_, err = s.SetParent(id["A"], id["B"], task.ActorHuman)
	require.Error(t, err, "A under its own child is a loop")
}

func TestSetParent_AtMostThreeLevels(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "L", "M", "N", "X", "S", "T")
	_, err := s.SetLong(id["L"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["M"], id["L"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["N"], id["M"], task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetParent(id["X"], id["N"], task.ActorHuman)
	require.Error(t, err, "a fourth level is refused")
	_, err = s.AddChild("Y", "", "", id["N"], nil, "", "", task.ActorHuman)
	require.Error(t, err, "also when adding")

	// A subtree counts as a whole: S carries T, so S under M would put T on level 4.
	_, err = s.SetParent(id["T"], id["S"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetParent(id["S"], id["M"], task.ActorHuman)
	require.Error(t, err, "moving a subtree below the limit is refused")
	_, err = s.SetParent(id["S"], id["L"], task.ActorHuman)
	require.NoError(t, err, "one level up it fits")
}

func TestDueRule_ChildMustBeStrictlyEarlierThanParent(t *testing.T) {
	cases := []struct {
		name        string
		child, par  string
		wantRefused bool
	}{
		{"earlier", dateIn(1), dateIn(2), false},
		{"equal", dateIn(2), dateIn(2), true},
		{"later", dateIn(3), dateIn(2), true},
		{"child without date", "", dateIn(2), false},
		{"parent without date", dateIn(3), "", false},
	}
	for _, c := range cases {
		t.Run("AddChild "+c.name, func(t *testing.T) {
			s := newStore(t)
			p, err := s.Add("親", "", c.par, task.ActorHuman)
			require.NoError(t, err)
			_, err = s.AddChild("子", "", c.child, p.ID, nil, "", "", task.ActorHuman)
			assertRule(t, err, c.wantRefused)
		})
		t.Run("SetParent "+c.name, func(t *testing.T) {
			s := newStore(t)
			p, err := s.Add("親", "", c.par, task.ActorHuman)
			require.NoError(t, err)
			k, err := s.Add("子", "", c.child, task.ActorHuman)
			require.NoError(t, err)
			_, err = s.SetParent(k.ID, p.ID, task.ActorHuman)
			assertRule(t, err, c.wantRefused)
		})
		t.Run("SetDue child "+c.name, func(t *testing.T) {
			s := newStore(t)
			p, err := s.Add("親", "", c.par, task.ActorHuman)
			require.NoError(t, err)
			k, err := s.AddChild("子", "", "", p.ID, nil, "", "", task.ActorHuman)
			require.NoError(t, err)
			_, err = s.SetDue(k.ID, c.child, task.ActorHuman)
			assertRule(t, err, c.wantRefused)
		})
		t.Run("SetDue parent "+c.name, func(t *testing.T) {
			s := newStore(t)
			p, err := s.Add("親", "", "", task.ActorHuman)
			require.NoError(t, err)
			_, err = s.AddChild("子", "", c.child, p.ID, nil, "", "", task.ActorHuman)
			require.NoError(t, err)
			_, err = s.SetDue(p.ID, c.par, task.ActorHuman)
			assertRule(t, err, c.wantRefused)
		})
	}
}

func assertRule(t *testing.T, err error, refused bool) {
	t.Helper()
	if refused {
		require.ErrorIs(t, err, task.ErrDueRule)
		return
	}
	require.NoError(t, err)
}

func TestDueRule_MessageNamesBothDatesAndClearingNeverErrors(t *testing.T) {
	s := newStore(t)
	p, err := s.Add("親", "", dateIn(2), task.ActorHuman)
	require.NoError(t, err)
	k, err := s.AddChild("子", "", dateIn(1), p.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)

	_, err = s.SetDue(k.ID, dateIn(2), task.ActorHuman)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "より前にする")
	assert.Contains(t, err.Error(), "親 "+p.ID)

	_, err = s.SetDue(k.ID, "", task.ActorHuman)
	require.NoError(t, err, "clearing the child's date")
	_, err = s.SetDue(p.ID, "", task.ActorHuman)
	require.NoError(t, err, "clearing the parent's date")
}

func TestAddChild_GoesDirectlyAboveAnOpenParentAndCopiesFocus(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "親", "B")
	kid, err := s.AddChild("子", "", "", id["親"], nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.False(t, kid.Focused)
	assert.Equal(t, []string{"A", "子", "親", "B"}, queueNames(t, s))

	_, err = s.SetFocus(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	kid2, err := s.AddChild("子2", "", "", id["親"], nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.True(t, kid2.Focused, "the child joins the parent's band")
	assert.Equal(t, []string{"子2", "親", "A", "子", "B"}, queueNames(t, s))

	// A parent outside the queue gives no place: the child goes to the end.
	w, err := s.Add("待ち", "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Wait(w.ID, "someone", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.AddChild("子3", "", "", w.ID, nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, "子3", queueNames(t, s)[5])
}

func TestUndo_AddChildPutsTheParentBack(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "親", "B")
	_, err := s.AddChild("子", "", "", id["親"], nil, "", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "親", "B"}, queueNames(t, s))
}

func TestSetParent_MovesAChildBelowItsParentDirectlyAboveIt(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "A", "B", "子")
	got, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "親", "A", "B"}, queueNames(t, s))
	assert.Equal(t, id["親"], got.Parent)

	// Already above: it stays where it is.
	s2 := newStore(t)
	id2 := addAll(t, s2, "子", "A", "親")
	_, err = s2.SetParent(id2["子"], id2["親"], task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "A", "親"}, queueNames(t, s2))
}

func TestUndo_SetParentPutsTheMovedChildBack(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "A", "B", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	c, err := s.Get(id["子"])
	require.NoError(t, err)
	assert.Empty(t, c.Parent)
	assert.Equal(t, []string{"親", "A", "B", "子"}, queueNames(t, s), "back at the bottom")
}

func TestReorder_RefusesAChildBelowItsParentAndAcceptsOneAbove(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "A", "親", "B")
	kid, err := s.AddChild("子", "", "", id["親"], nil, "", "", task.ActorHuman)
	require.NoError(t, err) // A 子 親 B

	_, err = s.Reorder(kid.ID, 3, 7, 0, task.ActorHuman)
	require.ErrorIs(t, err, task.ErrOrderRule, "child to the bottom, below its parent")
	assert.Equal(t, []string{"A", "子", "親", "B"}, queueNames(t, s), "nothing moved")

	_, err = s.Reorder(id["親"], 0, 7, 0, task.ActorHuman)
	require.ErrorIs(t, err, task.ErrOrderRule, "parent to the top, above its child")
	assert.Equal(t, []string{"A", "子", "親", "B"}, queueNames(t, s))

	_, err = s.Reorder(kid.ID, 0, 7, 0, task.ActorHuman)
	require.NoError(t, err, "up is fine")
	_, err = s.Reorder(id["親"], 3, 7, 0, task.ActorHuman)
	require.NoError(t, err, "parent down is fine")
	assert.Equal(t, []string{"子", "A", "B", "親"}, queueNames(t, s))
}

func TestFocusAt_TheButtonPlacesByTheRule(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "A", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"親", "子", "A"}, queueNames(t, s), "parent in the band, child below it in another band")

	// Unfocusing the parent would land it at the top of the list, above its
	// child: it goes directly below the child instead.
	_, err = s.SetFocus(id["親"], false, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "親", "A"}, queueNames(t, s))

	// Focusing the child behind a focused parent puts it directly above.
	_, err = s.SetFocus(id["親"], true, task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(id["子"], true, task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "親", "A"}, queueNames(t, s))
}

func TestFocusAt_NamedPlaceBreakingTheRuleIsRefusedAndUndoable(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "A", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.SetFocus(id["親"], true, task.ActorHuman)
	require.NoError(t, err) // 親 | 子 A

	_, err = s.FocusAt(id["親"], false, 0, 0, task.ActorHuman)
	require.ErrorIs(t, err, task.ErrOrderRule, "dropped above its child")
	p, _ := s.Get(id["親"])
	assert.True(t, p.Focused, "refused: still focused")
	assert.Equal(t, []string{"親", "子", "A"}, queueNames(t, s))

	_, err = s.FocusAt(id["親"], false, 3, 0, task.ActorHuman)
	require.NoError(t, err, "dropped below its child")
	assert.Equal(t, []string{"子", "A", "親"}, queueNames(t, s))

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"親", "子", "A"}, queueNames(t, s), "undo puts the parent back in the band")
}

func TestResumeAndReopen_LandAChildAboveItsOpenParent(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "親", "A", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err) // 子 親 A
	_, err = s.Wait(id["子"], "someone", "", task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Resume(id["子"], "", task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "親", "A"}, queueNames(t, s), "resume goes to the end, then above the parent")

	_, err = s.Done(id["子"], task.ActorHuman)
	require.NoError(t, err)
	_, err = s.Reopen(id["子"], task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"子", "親", "A"}, queueNames(t, s))
}

func TestUnfocusedBandRuleSurvivesAReload(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)
	id := addAll(t, s, "親", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	re := openAt(t, dir)
	assert.Equal(t, []string{"子", "親"}, queueNames(t, re))
}

// Unfocusing a parent lands it directly below its lowest child in one move,
// however many children it has.
func TestSetFocus_UnfocusingAParentWithManyChildrenKeepsThemAbove(t *testing.T) {
	for _, n := range []int{6, 7, 12} {
		s := newStore(t)
		id := addAll(t, s, "X", "親")
		_, err := s.SetFocus(id["親"], true, task.ActorHuman)
		require.NoError(t, err)
		for k := 0; k < n; k++ {
			name := string(rune('a' + k))
			_, err = s.AddChild(name, "", "", id["親"], nil, "", "", task.ActorHuman)
			require.NoError(t, err)
			// A child of a focused parent joins its band, above it.
		}
		tasks, _ := s.List()
		for _, x := range tasks {
			if x.Parent == id["親"] {
				_, err = s.SetFocus(x.ID, false, task.ActorHuman) // back to the lower band
				require.NoError(t, err)
			}
		}
		before := queueNames(t, s)
		_, err = s.SetFocus(id["親"], false, task.ActorHuman)
		require.NoErrorf(t, err, "%d children", n)

		got := queueNames(t, s)
		parentAt := -1
		for k, name := range got {
			if name == "親" {
				parentAt = k
			}
		}
		for k, name := range got {
			if len(name) == 1 && name != "X" {
				assert.Less(t, k, parentAt, "%d children: %s above the parent", n, name)
			}
		}

		_, err = s.Undo(task.ActorHuman)
		require.NoError(t, err)
		assert.Equal(t, before, queueNames(t, s), "%d children: undo puts the queue back", n)
	}
}

// Undoing SetParent puts back every task the tie moved, not only the child.
func TestUndo_SetParentPutsBackDescendantsItMoved(t *testing.T) {
	s := newStore(t)
	id := addAll(t, s, "P", "A", "G", "C")
	_, err := s.SetParent(id["G"], id["C"], task.ActorHuman)
	require.NoError(t, err)
	require.Equal(t, []string{"P", "A", "G", "C"}, queueNames(t, s))

	_, err = s.SetParent(id["C"], id["P"], task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"G", "C", "P", "A"}, queueNames(t, s))

	_, err = s.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"P", "A", "G", "C"}, queueNames(t, s))
}

// An event written before Moved existed still undoes: only the child goes back.
func TestUndo_SetParentEventWithoutMovedStillUndoes(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)
	id := addAll(t, s, "親", "A", "B", "子")
	_, err := s.SetParent(id["子"], id["親"], task.ActorHuman)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	files, err := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	require.NoError(t, err)
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		require.NoError(t, rerr)
		out := regexp.MustCompile(`,"moved":\[[^\]]*\]`).ReplaceAll(b, nil)
		require.NotEqual(t, string(b), string(out), "the event carried moved")
		require.NoError(t, os.WriteFile(f, out, 0o600))
	}

	re := openAt(t, dir)
	_, err = re.Undo(task.ActorHuman)
	require.NoError(t, err)
	assert.Equal(t, []string{"親", "A", "B", "子"}, queueNames(t, re))
}
