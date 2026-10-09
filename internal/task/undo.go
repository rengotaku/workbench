package task

import (
	"errors"
	"fmt"
	"math"
)

// ErrNothingToUndo is returned when no reversible change is left.
var ErrNothingToUndo = errors.New("nothing to undo")

// Undone describes what an undo put back, so the caller can say it out loud.
type Undone struct {
	EventID string `json:"event_id"`
	TaskID  string `json:"task_id"`
	Kind    Kind   `json:"kind"`
	Summary string `json:"summary"`
}

// Undo reverses the most recent change that has not been reversed already, and
// keeps walking backwards on repeated calls.
//
// The reversal is applied in place and recorded as its own event: the log is
// append-only, so an undo adds a line rather than erasing one. What was undone
// is remembered by event ID, which is what makes the second undo step further
// back instead of toggling the same change forever.
func (s *Store) Undo(actor Actor) (Undone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events, err := s.readEvents()
	if err != nil {
		return Undone{}, err
	}

	done := make(map[string]bool, len(s.st.Undone))
	for _, id := range s.st.Undone {
		done[id] = true
	}

	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if done[e.ID] || !undoable(e.Kind) {
			continue
		}
		summary, err := s.reverse(e, actor)
		if errors.Is(err, errSkipUndo) {
			continue // the task is gone, or the event cannot be applied now
		}
		if err != nil {
			return Undone{}, err
		}

		s.st.Undone = append(s.st.Undone, e.ID)
		if err := s.persist(); err != nil {
			return Undone{}, err
		}
		return Undone{EventID: e.ID, TaskID: e.TaskID, Kind: e.Kind, Summary: summary}, nil
	}
	return Undone{}, ErrNothingToUndo
}

// errSkipUndo means this event cannot be reversed now; keep looking further back.
var errSkipUndo = errors.New("skip")

// recordSpent writes the event a reversal produces and marks it as already
// undone. Without this, undoing twice would put the change back and then take
// it away again — a toggle instead of a walk backwards through the history.
func (s *Store) recordSpent(e Event) error {
	id, err := s.recordID(e)
	if err != nil {
		return err
	}
	s.st.Undone = append(s.st.Undone, id)
	return nil
}

func undoable(k Kind) bool {
	switch k {
	case KindCreated, KindRemoved, KindRenamed, KindDetail, KindDueSet,
		KindLinked, KindDone, KindReopened, KindWaiting, KindResumed,
		KindShelved, KindReordered, KindFocused, KindUnfocused:
		return true
	default:
		return false
	}
}

// reverse applies the inverse of one event. It runs with the lock already held,
// so it changes the state directly rather than calling the public methods.
func (s *Store) reverse(e Event, actor Actor) (string, error) {
	now := s.now()

	// Putting a removed task back is the only case with no task to look up.
	if e.Kind == KindRemoved {
		if e.Snapshot == nil {
			return "", errSkipUndo // removed before snapshots were kept
		}
		if _, err := s.indexOf(e.TaskID); err == nil {
			return "", errSkipUndo // an ID this old is in use again
		}
		back := *e.Snapshot
		back.Order = math.MaxInt32
		s.st.Tasks = append(s.st.Tasks, back)
		s.renumber()
		if err := s.recordSpent(Event{TaskID: back.ID, Kind: KindCreated, Actor: actor, ToValue: back.Name}); err != nil {
			return "", err
		}
		return fmt.Sprintf("削除を取り消した: %s %s", back.ID, back.Name), nil
	}

	i, err := s.indexOf(e.TaskID)
	if err != nil {
		return "", errSkipUndo // the task was removed since
	}
	t := s.st.Tasks[i]

	switch e.Kind {
	case KindCreated:
		s.st.Tasks = append(s.st.Tasks[:i], s.st.Tasks[i+1:]...)
		s.renumber()
		snapshot := t
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindRemoved, Actor: actor,
			ToValue: t.Name, Snapshot: &snapshot}); err != nil {
			return "", err
		}
		return fmt.Sprintf("追加を取り消した: %s %s", t.ID, t.Name), nil

	case KindRenamed:
		if e.FromValue == "" {
			return "", errSkipUndo
		}
		t.Name = e.FromValue
		s.st.Tasks[i] = t
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindRenamed, Actor: actor,
			FromValue: e.ToValue, ToValue: e.FromValue}); err != nil {
			return "", err
		}
		return fmt.Sprintf("名前を戻した: %s %s", t.ID, t.Name), nil

	case KindDetail:
		t.Detail = e.FromValue
		s.st.Tasks[i] = t
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindDetail, Actor: actor,
			FromValue: e.ToValue, ToValue: e.FromValue}); err != nil {
			return "", err
		}
		return fmt.Sprintf("詳細を戻した: %s %s", t.ID, t.Name), nil

	case KindLinked:
		t.URL = e.FromValue
		if e.FromValue == "" {
			t.LinkText = ""
		}
		s.st.Tasks[i] = t
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindLinked, Actor: actor,
			FromValue: e.ToValue, ToValue: e.FromValue}); err != nil {
			return "", err
		}
		return fmt.Sprintf("リンクを戻した: %s %s", t.ID, t.Name), nil

	case KindDueSet:
		t.Due = e.FromValue
		s.st.Tasks[i] = t
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindDueSet, Actor: actor,
			FromValue: e.ToValue, ToValue: e.FromValue}); err != nil {
			return "", err
		}
		shown := t.Due
		if shown == "" {
			shown = "なし"
		}
		return fmt.Sprintf("期限を戻した: %s %s（%s）", t.ID, t.Name, shown), nil

	case KindDone:
		if t.State != StateDone {
			return "", errSkipUndo
		}
		t.State = StateOpen
		t.CompletedAt = nil
		t.Order = math.MaxInt32
		s.st.Tasks[i] = t
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindReopened, Actor: actor}); err != nil {
			return "", err
		}
		return fmt.Sprintf("完了を取り消した: %s %s", t.ID, t.Name), nil

	case KindReopened:
		if t.State == StateDone {
			return "", errSkipUndo
		}
		t.State = StateDone
		t.CompletedAt = &now
		s.st.Tasks[i] = t
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindDone, Actor: actor}); err != nil {
			return "", err
		}
		return fmt.Sprintf("完了に戻した: %s %s", t.ID, t.Name), nil

	case KindWaiting:
		if t.State != StateWaiting {
			return "", errSkipUndo
		}
		from := t.HandedTo
		t.State = StateOpen
		t.HandedTo = ""
		t.HandedOn = ""
		t.WaitNote = ""
		s.st.Tasks[i] = t
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindResumed, Actor: actor, FromValue: from}); err != nil {
			return "", err
		}
		return fmt.Sprintf("待ちを取り消した: %s %s", t.ID, t.Name), nil

	case KindResumed:
		if e.FromValue == "" {
			return "", errSkipUndo // no record of who held it
		}
		t.State = StateWaiting
		t.HandedTo = e.FromValue
		if t.HandedOn == "" {
			t.HandedOn = now.Format(DateLayout)
		}
		s.st.Tasks[i] = t
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindWaiting, Actor: actor, ToValue: e.FromValue}); err != nil {
			return "", err
		}
		return fmt.Sprintf("待ちに戻した: %s %s → %s", t.ID, t.Name, t.HandedTo), nil

	case KindFocused, KindUnfocused:
		on := e.Kind == KindUnfocused
		if t.Focused == on {
			return "", errSkipUndo
		}
		t.Focused = on
		s.st.Tasks[i] = t
		back := KindUnfocused
		if on {
			back = KindFocused
		}
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: back, Actor: actor}); err != nil {
			return "", err
		}
		if on {
			return fmt.Sprintf("フォーカスに戻した: %s %s", t.ID, t.Name), nil
		}
		return fmt.Sprintf("フォーカスを外した: %s %s", t.ID, t.Name), nil

	case KindShelved:
		if t.State != StateSomeday || e.Snapshot == nil {
			return "", errSkipUndo
		}
		back := *e.Snapshot
		back.ID = t.ID
		if back.State == StateSomeday {
			// Extending someday only moved Until; undoing it only moves Until
			// back — the task stays someday, not a resume it never asked for.
			s.st.Tasks[i] = back
			if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindShelved, Actor: actor,
				FromValue: e.ToValue, ToValue: back.Until}); err != nil {
				return "", err
			}
			return fmt.Sprintf("いつかの期限を戻した: %s %s（%s）", t.ID, t.Name, back.Until), nil
		}
		s.st.Tasks[i] = back
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindResumed, Actor: actor, FromValue: back.HandedTo}); err != nil {
			return "", err
		}
		return fmt.Sprintf("棚上げを取り消した: %s %s", t.ID, t.Name), nil

	case KindReordered:
		if t.State != StateOpen {
			return "", errSkipUndo
		}
		open := s.openIndexes()
		to := e.FromOrder
		if to >= len(open) {
			to = len(open) - 1
		}
		if to < 0 {
			return "", errSkipUndo
		}
		from := t.Order
		s.move(open, i, to)
		s.renumber()
		if err := s.recordSpent(Event{TaskID: t.ID, Kind: KindReordered, Actor: actor,
			FromOrder: from, ToOrder: to}); err != nil {
			return "", err
		}
		return fmt.Sprintf("順序を戻した: %s %s → %d 番目", t.ID, t.Name, to+1), nil
	}
	return "", errSkipUndo
}

// UndoableCount reports how many reversible changes are still stacked up. The
// page uses it to decide whether to offer the action at all.
func (s *Store) UndoableCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events, err := s.readEvents()
	if err != nil {
		return 0, err
	}
	done := make(map[string]bool, len(s.st.Undone))
	for _, id := range s.st.Undone {
		done[id] = true
	}

	n := 0
	for _, e := range events {
		if !done[e.ID] && undoable(e.Kind) {
			n++
		}
	}
	return n, nil
}
