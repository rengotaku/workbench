package task

import (
	"fmt"
	"strings"
)

// SetLong moves a queue task to the long-run band, or brings a long-run task
// back (ADR todo-0019). Long-run is a state: the task leaves the queue and the
// focus band, so a months-long job stops taking a focus slot. Coming back puts
// it at the top of the queue, just below the focused block, the same place an
// unfocused task goes.
func (s *Store) SetLong(id string, on bool, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	before := s.st.Tasks[i]
	if on && before.State == StateLong || !on && before.State == StateOpen {
		return before, nil
	}
	if on && before.State != StateOpen {
		return Task{}, fmt.Errorf("%s is %s: only a queue task can become long-run", id, before.State)
	}
	if !on && before.State != StateLong {
		return Task{}, fmt.Errorf("%s is not long-run", id)
	}
	if on && before.Parent != "" {
		// One level only: a long-run task is a parent, never a child.
		return Task{}, fmt.Errorf("%s is a child of %s: unlink it before making it long-run", id, before.Parent)
	}

	t := before
	ev := Event{TaskID: id, Actor: actor, FromOrder: before.Order}
	if on {
		t.State = StateLong
		t.Focused = false
		s.st.Tasks[i] = t
		s.renumber()
		snapshot := before
		ev.Kind, ev.Snapshot = KindLonged, &snapshot
	} else {
		t.State = StateOpen
		s.st.Tasks[i] = t
		open := s.openIndexes()
		s.move(open, i, s.clampToBlock(open, i, -1))
		s.renumber()
		ev.Kind, ev.ToOrder = KindUnlonged, s.st.Tasks[i].Order
	}
	if err := s.record(ev); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// SetParent ties a task to a long-run task, or unties it when parent is empty.
// The parent has to be long-run at the time of tying; the link is kept if the
// parent later stops being long-run, and the page simply stops showing it.
func (s *Store) SetParent(id, parent string, actor Actor) (Task, error) {
	parent = strings.TrimSpace(parent)

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if err := s.checkParent(t, parent); err != nil {
		return Task{}, err
	}
	if t.Parent == parent {
		return t, nil
	}

	from := t.Parent
	t.Parent = parent
	s.st.Tasks[i] = t
	if err := s.record(Event{TaskID: id, Kind: KindParentSet, Actor: actor, FromValue: from, ToValue: parent}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

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
	if s.st.Tasks[j].State != StateLong {
		return fmt.Errorf("%s is not long-run: only a long-run task can be a parent", parent)
	}
	return nil
}
