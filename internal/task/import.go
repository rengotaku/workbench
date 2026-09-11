package task

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ImportItem is one row to bring over from the old ledgers. ID is optional:
// rows that already had a number keep it so earlier references still point at
// the same task, and rows without one are numbered after the highest ID in use.
type ImportItem struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Due    string `json:"due,omitempty"`
	Done   bool   `json:"done,omitempty"`
}

// ErrNotEmpty is returned when an import would land on top of existing tasks.
var ErrNotEmpty = errors.New("store already holds tasks")

var idPattern = regexp.MustCompile(`^T(\d{3,})$`)

// Import brings the old ledgers over in one go: the queue ends up in the order
// the items are given, explicit IDs are preserved, and completed rows arrive
// already done.
//
// It refuses to run against a store that already holds tasks, because a second
// import would duplicate every row. Everything is validated before anything is
// written, so a bad row leaves the store untouched.
func (s *Store) Import(items []ImportItem, actor Actor) ([]Task, error) {
	if len(items) == 0 {
		return nil, errors.New("nothing to import")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.st.Tasks) > 0 {
		return nil, fmt.Errorf("%w (%d): import once, into an empty store", ErrNotEmpty, len(s.st.Tasks))
	}

	// --- validate everything first ---
	seen := make(map[string]bool, len(items))
	highest := 0
	for i, it := range items {
		if it.Name == "" {
			return nil, fmt.Errorf("item %d has no name", i+1)
		}
		if it.Due != "" {
			if _, err := ParseDate(it.Due, s.loc); err != nil {
				return nil, fmt.Errorf("item %d (%s): %w", i+1, it.Name, err)
			}
		}
		if it.ID == "" {
			continue
		}
		m := idPattern.FindStringSubmatch(it.ID)
		if m == nil {
			return nil, fmt.Errorf("item %d has an ID that is not T + digits: %q", i+1, it.ID)
		}
		if seen[it.ID] {
			return nil, fmt.Errorf("ID %s appears twice", it.ID)
		}
		seen[it.ID] = true

		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("item %d has an unreadable ID %q: %w", i+1, it.ID, err)
		}
		if n > highest {
			highest = n
		}
	}

	// Numbers already spent in the old ledger are never handed out again, so
	// auto-numbered rows start after the highest one.
	if highest >= s.st.NextTask {
		s.st.NextTask = highest + 1
	}

	// --- apply ---
	now := s.now()
	for _, it := range items {
		id := it.ID
		if id == "" {
			id = fmt.Sprintf("T%03d", s.st.NextTask)
			s.st.NextTask++
		}

		t := Task{
			ID:        id,
			Name:      it.Name,
			Detail:    it.Detail,
			Due:       it.Due,
			State:     StateOpen,
			CreatedAt: now,
		}
		if it.Done {
			done := now
			t.State = StateDone
			t.CompletedAt = &done
		}
		s.st.Tasks = append(s.st.Tasks, t)

		if err := s.record(Event{TaskID: id, Kind: KindCreated, Actor: actor, ToValue: it.Name}); err != nil {
			return nil, err
		}
		if it.Done {
			if err := s.record(Event{TaskID: id, Kind: KindDone, Actor: actor}); err != nil {
				return nil, err
			}
		}
	}

	s.renumber()
	if err := s.persist(); err != nil {
		return nil, err
	}
	return s.snapshot(), nil
}
