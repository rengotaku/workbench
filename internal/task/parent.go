package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxDepth is how many levels a tree of parents and children may have, the
// top task included (ADR todo-0020): a long-run task, the main action under it
// and a step under that.
const MaxDepth = 3

// Rule errors are a task breaking the parent/child rules rather than a bad
// request: the caller shows the message and keeps the list as it was.
var (
	// ErrDueRule means a child's due date is not earlier than its parent's.
	ErrDueRule = errors.New("child due must be earlier than parent due")
	// ErrOrderRule means the queue order would put a child below its parent.
	ErrOrderRule = errors.New("a child must sit above its parent")
)

// ruleError carries a message for the person and the sentinel it matches.
type ruleError struct {
	kind error
	msg  string
}

func (e *ruleError) Error() string        { return e.msg }
func (e *ruleError) Is(target error) bool { return target == e.kind }

// SetParent ties a task to a parent, or unties it when parent is empty. Any
// task that is not done may be a parent; a long-run task may not be a child.
// The depth, the due date and the queue order are checked and kept (ADR
// todo-0020): the link is kept if the parent later finishes, and the page
// simply shows it as it is.
func (s *Store) SetParent(id, parent string, actor Actor) (Task, error) {
	parent = strings.TrimSpace(parent)

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.Parent == parent {
		return t, nil
	}
	if err := s.checkParent(t, parent); err != nil {
		return Task{}, err
	}

	from := t.Parent
	t.Parent = parent
	s.st.Tasks[i] = t
	ev := Event{TaskID: id, Kind: KindParentSet, Actor: actor, FromValue: from, ToValue: parent}
	if parent != "" && t.State == StateOpen {
		// The child has to sit above an open parent in its band; if it was
		// below, it moves directly above it. The old place is kept for undo.
		if p := s.st.Tasks[s.mustIndex(parent)]; p.State == StateOpen && p.Focused == t.Focused && t.Order > p.Order {
			before := s.openOrders()
			open := s.openIndexes()
			s.move(open, i, p.Order)
			s.renumber()
			ev.FromOrder, ev.ToOrder = t.Order, s.st.Tasks[i].Order
			ev.Moved = s.movedSince(before)
		}
	}
	if err := s.record(ev); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// checkParent says whether t may be tied to parent. t.ID is empty for a task
// that is about to be created; its Due is the one it will be created with.
func (s *Store) checkParent(t Task, parent string) error {
	if parent == "" {
		return nil
	}
	if t.ID != "" && parent == t.ID {
		return fmt.Errorf("%s cannot be its own parent", t.ID)
	}
	if t.State == StateLong {
		return fmt.Errorf("%s is long-run: a long-run task cannot be a child", t.ID)
	}
	j, err := s.indexOf(parent)
	if err != nil {
		return err
	}
	p := s.st.Tasks[j]
	if p.State == StateDone {
		return fmt.Errorf("%s is done: a finished task cannot be a parent", parent)
	}

	up := 0 // levels from the parent to the top, the parent included
	for cur, hops := p, 0; ; hops++ {
		up++
		if t.ID != "" && cur.ID == t.ID {
			return fmt.Errorf("%s is above %s: tying them would make a loop", t.ID, parent)
		}
		if cur.Parent == "" || hops > MaxDepth*4 {
			break
		}
		k, err := s.indexOf(cur.Parent)
		if err != nil {
			break // the link points at a task that is gone
		}
		cur = s.st.Tasks[k]
	}
	height := 1
	if t.ID != "" {
		height = s.height(t.ID, 0)
	}
	if up+height > MaxDepth {
		return fmt.Errorf("親子は %d 段までにする（ここに置くと %d 段になる）", MaxDepth, up+height)
	}
	return dueRule(t, p)
}

// height is the number of levels in the tree under id, id included.
func (s *Store) height(id string, hops int) int {
	if hops > MaxDepth*4 {
		return 1
	}
	deepest := 0
	for _, c := range s.st.Tasks {
		if c.Parent == id {
			deepest = max(deepest, s.height(c.ID, hops+1))
		}
	}
	return deepest + 1
}

// dueRule is the due-date rule between one child and its parent: when both
// have a date the child's is strictly earlier. A missing date, or a task that
// is done, constrains nothing.
func dueRule(child, parent Task) error {
	if child.Due == "" || parent.Due == "" || child.State == StateDone || parent.State == StateDone {
		return nil
	}
	if child.Due < parent.Due {
		return nil
	}
	who := "子"
	if child.ID != "" {
		who = "子 " + child.ID + " "
	}
	return &ruleError{kind: ErrDueRule, msg: fmt.Sprintf("%sの期限（%s）は親 %s の期限（%s）より前にする",
		who, shortDate(child.Due), parent.ID, shortDate(parent.Due))}
}

// shortDate writes YYYY-MM-DD as M/D, the way the page does.
func shortDate(due string) string {
	d, err := time.Parse(DateLayout, due)
	if err != nil {
		return due
	}
	return fmt.Sprintf("%d/%d", int(d.Month()), d.Day())
}

// checkDue is dueRule for a task whose due date is about to change: against
// its parent, and against each child.
func (s *Store) checkDue(t Task) error {
	if t.Parent != "" {
		if j, err := s.indexOf(t.Parent); err == nil {
			if err := dueRule(t, s.st.Tasks[j]); err != nil {
				return err
			}
		}
	}
	for _, c := range s.st.Tasks {
		if c.Parent == t.ID {
			if err := dueRule(c, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) mustIndex(id string) int {
	i, err := s.indexOf(id)
	if err != nil {
		return 0
	}
	return i
}

// orderViolation finds an open child that sits below its open parent in the
// same band (ADR todo-0020). A child and a parent in different bands, or a
// parent outside the queue, never conflict.
func (s *Store) orderViolation() (child, parent Task, found bool) {
	for _, i := range s.openIndexes() {
		c := s.st.Tasks[i]
		if c.Parent == "" {
			continue
		}
		j, err := s.indexOf(c.Parent)
		if err != nil {
			continue
		}
		if p := s.st.Tasks[j]; p.State == StateOpen && p.Focused == c.Focused && c.Order > p.Order {
			return c, p, true
		}
	}
	return Task{}, Task{}, false
}

// openOrders notes where every open task sits now.
func (s *Store) openOrders() map[string]int {
	out := map[string]int{}
	for _, i := range s.openIndexes() {
		out[s.st.Tasks[i].ID] = s.st.Tasks[i].Order
	}
	return out
}

// movedSince lists the open tasks whose position differs from before, with
// the position they had, in queue order.
func (s *Store) movedSince(before map[string]int) []MovedTask {
	var out []MovedTask
	for _, i := range s.openIndexes() {
		t := s.st.Tasks[i]
		if was, ok := before[t.ID]; ok && was != t.Order {
			out = append(out, MovedTask{ID: t.ID, Order: was})
		}
	}
	return out
}

func orderRule(child, parent Task) error {
	return &ruleError{kind: ErrOrderRule, msg: fmt.Sprintf("子 %s は親 %s より上に置く", child.ID, parent.ID)}
}

// placeByRule moves the open task at i, when it breaks the order rule, to the
// nearest place that keeps it: directly above its parent, or directly below
// its lowest child. It reports whether the queue is clean afterwards. Used
// where the caller named no place (the focus button).
func (s *Store) placeByRule(i int) bool {
	self := s.st.Tasks[i]
	for range len(s.st.Tasks) + 1 {
		c, p, found := s.orderViolation()
		if !found {
			return true
		}
		open := s.openIndexes()
		switch {
		case c.ID == self.ID:
			s.move(open, i, p.Order)
		case p.ID == self.ID:
			// Directly below its lowest child in the band, in one move.
			lowest := c.Order
			for _, j := range open {
				if k := s.st.Tasks[j]; k.Parent == self.ID && k.Focused == self.Focused {
					lowest = max(lowest, k.Order)
				}
			}
			s.move(open, i, lowest)
		default:
			return false
		}
	}
	_, _, found := s.orderViolation()
	return !found
}

// childrenFirst puts every child ahead of its parent within one band, moving a
// child to just before the parent when it was after. Tasks that need no move
// keep their places, so the result is the same for the same input.
func (s *Store) childrenFirst(group []int) []int {
	out := append([]int(nil), group...)
	at := make(map[string]int, len(out))
	reindex := func() {
		for k, i := range out {
			at[s.st.Tasks[i].ID] = k
		}
	}
	reindex()
	for pass := 0; pass <= len(out); pass++ {
		moved := false
		for k := 0; k < len(out); k++ {
			pk, ok := at[s.st.Tasks[out[k]].Parent]
			if !ok || pk > k {
				continue
			}
			x := out[k]
			copy(out[pk+1:k+1], out[pk:k])
			out[pk] = x
			reindex()
			moved = true
		}
		if !moved {
			break
		}
	}
	return out
}
