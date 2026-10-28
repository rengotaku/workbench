package task

import "fmt"

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
		// A long-run task is the top of a tree, never a child.
		return Task{}, fmt.Errorf("%s is a child of %s: unlink it before making it long-run", id, before.Parent)
	}

	t := before
	ev := Event{TaskID: id, Actor: actor, FromOrder: before.Order}
	if on {
		t.State = StateLong
		t.Focused = false
		if t.ReviewOn == "" {
			t.ReviewOn = s.today().AddDate(0, 0, DefaultReviewDays).Format(DateLayout)
		}
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

// DefaultReviewDays is how far ahead the next review goes when none is given.
const DefaultReviewDays = 7

// Review sets the day a long-run task is next looked at, empty meaning a week
// from today, and notes that somebody has just looked at it (ADR todo-0022).
func (s *Store) Review(id, on string, actor Actor) (Task, error) {
	if on != "" {
		if _, err := ParseDate(on, s.loc); err != nil {
			return Task{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.State != StateLong {
		return Task{}, fmt.Errorf("%s is %s: only a long-run task is reviewed", id, t.State)
	}
	if on == "" {
		on = s.today().AddDate(0, 0, DefaultReviewDays).Format(DateLayout)
	}

	snapshot := t
	t.ReviewOn = on
	// Only a person's review counts as a touch: an AI moving the date must not
	// hide a long-run task that nobody has looked at (ADR todo-0021).
	if actor == ActorHuman {
		now := s.now()
		t.ReviewedAt = &now
	}
	s.st.Tasks[i] = t
	if err := s.record(Event{TaskID: id, Kind: KindReviewSet, Actor: actor, FromValue: snapshot.ReviewOn, ToValue: on, Snapshot: &snapshot}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}
