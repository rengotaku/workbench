package task

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when an ID does not exist. Callers surface it as an
// error rather than a silent no-op, so a typo never looks like success.
var ErrNotFound = errors.New("not found")

// ErrAlreadyDone is returned when a task is completed twice.
var ErrAlreadyDone = errors.New("already done")

// ErrStale is returned when a caller acts on a list that has changed under it.
// Reordering is the case that matters: a position means nothing once the rows
// have moved, so the request is refused instead of landing somewhere else.
var ErrStale = errors.New("the list changed since it was read")

// state is the on-disk shape of tasks.json.
type state struct {
	Tasks     []Task `json:"tasks"`
	NextTask  int    `json:"next_task"`  // next T number; never reuses a removed one
	NextEvent int    `json:"next_event"` // next E number
	Revision  int    `json:"revision"`   // bumped on every write; readers poll it
}

// Store keeps the current tasks in tasks.json and every change in
// events-YYYY-MM.jsonl beside it. The state file is rewritten atomically; the
// event log is only ever appended to.
type Store struct {
	loc *time.Location
	dir string
	st  state
	mu  sync.Mutex
}

// Open loads a store from dir, creating an empty one when nothing is there.
func Open(dir string, loc *time.Location) (*Store, error) {
	if loc == nil {
		loc = time.Local
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	s := &Store{dir: dir, loc: loc, st: state{NextTask: 1, NextEvent: 1}}

	b, err := os.ReadFile(s.statePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", s.statePath(), err)
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.statePath(), err)
	}
	if s.st.NextTask < 1 {
		s.st.NextTask = 1
	}
	if s.st.NextEvent < 1 {
		s.st.NextEvent = 1
	}
	return s, nil
}

func (s *Store) statePath() string { return filepath.Join(s.dir, "tasks.json") }

func (s *Store) eventsPath(at time.Time) string {
	return filepath.Join(s.dir, fmt.Sprintf("events-%s.jsonl", at.In(s.loc).Format("2006-01")))
}

// SetNextTaskNumber fixes where task IDs continue from. Migration uses it to
// carry on from the numbers already spent in the old ledger.
func (s *Store) SetNextTaskNumber(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < s.st.NextTask {
		return fmt.Errorf("next task number would go backwards: have %d, got %d", s.st.NextTask, n)
	}
	s.st.NextTask = n
	return s.persist()
}

// List returns copies of every task, ordered by their place in the queue,
// together with the revision they were read at.
func (s *Store) List() ([]Task, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(), s.st.Revision
}

// Revision reports the current revision. It is cheap enough to poll.
func (s *Store) Revision() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Revision
}

// Get returns a copy of one task.
func (s *Store) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	return s.st.Tasks[i], nil
}

// Add appends a task to the end of the queue.
func (s *Store) Add(name, detail, due string, actor Actor) (Task, error) {
	if name == "" {
		return Task{}, errors.New("name is required")
	}
	if due != "" {
		if _, err := ParseDate(due, s.loc); err != nil {
			return Task{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t := Task{
		ID:        fmt.Sprintf("T%03d", s.st.NextTask),
		Name:      name,
		Detail:    detail,
		Due:       due,
		State:     StateOpen,
		Order:     len(s.openIndexes()),
		CreatedAt: s.now(),
	}
	s.st.NextTask++
	s.st.Tasks = append(s.st.Tasks, t)
	s.renumber()

	if err := s.record(Event{TaskID: t.ID, Kind: KindCreated, Actor: actor, ToValue: name}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(t.ID), nil
}

// Done completes a task. Completing one twice is an error, so a mistyped ID
// cannot pass for a successful run.
func (s *Store) Done(id string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	if s.st.Tasks[i].State == StateDone {
		return Task{}, fmt.Errorf("%s: %w", id, ErrAlreadyDone)
	}

	now := s.now()
	t := s.st.Tasks[i]
	t.State = StateDone
	t.CompletedAt = &now
	s.st.Tasks[i] = t
	s.renumber()

	if err := s.record(Event{TaskID: id, Kind: KindDone, Actor: actor}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Wait moves a task out of the queue and records who holds the ball. The
// handoff date is the origin of the elapsed count and is not reset by a later
// chase, so how long something has sat stays readable.
func (s *Store) Wait(id, to, note string, actor Actor) (Task, error) {
	if to == "" {
		return Task{}, errors.New("--to is required: who holds the ball")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.State == StateDone {
		return Task{}, fmt.Errorf("%s is done: %w", id, ErrAlreadyDone)
	}

	t.State = StateWaiting
	t.HandedTo = to
	t.WaitNote = note
	if t.HandedOn == "" {
		t.HandedOn = s.now().Format(DateLayout)
	}
	s.st.Tasks[i] = t
	s.renumber()

	if err := s.record(Event{TaskID: id, Kind: KindWaiting, Actor: actor, ToValue: to}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Resume brings a waiting task back into the queue and clears the handoff, so
// a future handoff starts its own count.
func (s *Store) Resume(id, note string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.State != StateWaiting {
		return Task{}, fmt.Errorf("%s is not waiting", id)
	}

	from := t.HandedTo
	t.State = StateOpen
	t.HandedTo = ""
	t.HandedOn = ""
	t.WaitNote = ""
	s.st.Tasks[i] = t
	s.renumber()

	if err := s.record(Event{TaskID: id, Kind: KindResumed, Actor: actor, FromValue: from, ToValue: note}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Remove deletes a task. This is the one operation that loses something, so
// the name and due date are written into the removal event: the task is gone
// from the list, but the log still says what was taken out and when.
//
// Its other events stay in the log as orphans. Readers skip events whose task
// no longer exists, so nothing downstream breaks.
func (s *Store) Remove(id string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	removed := s.st.Tasks[i]

	s.st.Tasks = append(s.st.Tasks[:i], s.st.Tasks[i+1:]...)
	s.renumber()

	ev := Event{TaskID: id, Kind: KindRemoved, Actor: actor, ToValue: removed.Name}
	if removed.Due != "" {
		ev.FromValue = "due:" + removed.Due
	}
	if err := s.record(ev); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return removed, nil
}

// Reopen puts a completed task back into the queue, at the end of it. It is
// the way out of a mis-click on the checkbox: completion is a state, not a
// one-way door.
func (s *Store) Reopen(id string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.State != StateDone {
		return Task{}, fmt.Errorf("%s is not done", id)
	}

	t.State = StateOpen
	t.CompletedAt = nil
	t.Order = math.MaxInt32 // renumber puts it at the end of the queue
	s.st.Tasks[i] = t
	s.renumber()

	if err := s.record(Event{TaskID: id, Kind: KindReopened, Actor: actor}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// SetDue sets or clears a due date. Passing an empty string clears it.
func (s *Store) SetDue(id, due string, actor Actor) (Task, error) {
	if due != "" {
		if _, err := ParseDate(due, s.loc); err != nil {
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
	from := t.Due
	t.Due = due
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindDueSet, Actor: actor, FromValue: from, ToValue: due}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// SetName replaces the task name. The old name goes into the event, so a
// rename does not erase what the task used to be called.
func (s *Store) SetName(id, name string, actor Actor) (Task, error) {
	if name == "" {
		return Task{}, errors.New("name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	from := t.Name
	if from == name {
		return t, nil
	}
	t.Name = name
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindRenamed, Actor: actor, FromValue: from, ToValue: name}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// SetDetail replaces the detail text.
func (s *Store) SetDetail(id, detail string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	t.Detail = detail
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindDetail, Actor: actor}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Reorder moves one open task to position to (0-based) in the queue and
// records what it overtook. scopeDays sets the window that decides whether an
// overtaken due date is close enough to be worth explaining.
//
// expectRevision guards against acting on a list that has moved: pass the
// revision the positions were read at, or 0 to skip the check. A position is
// only meaningful against the order it was read from, so a mismatch is an
// error rather than a move to the wrong place.
func (s *Store) Reorder(id string, to, scopeDays, expectRevision int, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if expectRevision != 0 && expectRevision != s.st.Revision {
		return Task{}, fmt.Errorf("%w (read at %d, now %d)", ErrStale, expectRevision, s.st.Revision)
	}

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	if s.st.Tasks[i].State != StateOpen {
		return Task{}, fmt.Errorf("%s is not in the queue", id)
	}

	open := s.openIndexes()
	if to < 0 || to >= len(open) {
		return Task{}, fmt.Errorf("position out of range: %d (queue holds %d)", to, len(open))
	}

	from := s.st.Tasks[i].Order
	if from == to {
		return s.st.Tasks[i], nil
	}

	// Tasks passed on the way up are the ones a due date could argue against.
	var jumped []JumpedTask
	if to < from {
		today := s.today()
		for _, idx := range open {
			t := s.st.Tasks[idx]
			if t.ID == id || t.Order < to || t.Order >= from {
				continue
			}
			if within, _ := dueWithin(t.Due, today, scopeDays, s.loc); within {
				jumped = append(jumped, JumpedTask{ID: t.ID, Name: t.Name, Due: t.Due})
			}
		}
	}

	s.move(open, i, to)
	s.renumber()

	ev := Event{TaskID: id, Kind: KindReordered, Actor: actor, FromOrder: from, ToOrder: to, Jumped: jumped}
	if err := s.record(ev); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Events returns every recorded event for one task, oldest first.
func (s *Store) Events(taskID string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.indexOf(taskID); err != nil {
		return nil, err
	}
	all, err := s.readEvents()
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, 8)
	for _, e := range all {
		if e.TaskID == taskID {
			out = append(out, e)
		}
	}
	return out, nil
}

// Pending is one unexplained change, paired with the trigger that put it in
// the queue, so a question can quote the reason it is being asked.
type Pending struct {
	Trigger string `json:"trigger"`
	Task    Task   `json:"task"`
	Event   Event  `json:"event"`
}

// Ask returns the changes worth explaining that still have no reason: the
// three reorder shapes, plus waiting tasks that have stalled past
// AskThresholdDays.
func (s *Store) Ask() ([]Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readEvents()
	if err != nil {
		return nil, err
	}
	queueLen := len(s.openIndexes())
	today := s.today()

	var out []Pending
	for _, e := range all {
		i, err := s.indexOf(e.TaskID)
		if err != nil {
			continue // task removed by hand; nothing to ask about
		}
		if e.NeedsReason(queueLen) {
			out = append(out, Pending{Event: e, Task: s.st.Tasks[i], Trigger: e.AskReason(queueLen)})
		}
	}
	for _, idx := range s.waitingIndexes() {
		t := s.st.Tasks[idx]
		days, ok := t.StalledFor(today)
		if !ok || days < AskThresholdDays {
			continue
		}
		last, found := lastEventOfKind(all, t.ID, KindWaiting)
		if !found || last.Reason != "" {
			continue
		}
		out = append(out, Pending{
			Event:   last,
			Task:    t,
			Trigger: fmt.Sprintf("待ちに %d 日置いたまま", days),
		})
	}
	return out, nil
}

// Why writes a reason onto one event. Rewriting an explanation that is already
// there is refused, so an answer cannot be overwritten by accident.
func (s *Store) Why(eventID, reason string) (Event, error) {
	if reason == "" {
		return Event{}, errors.New("reason is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readEvents()
	if err != nil {
		return Event{}, err
	}
	var found *Event
	for i := range all {
		if all[i].ID == eventID {
			found = &all[i]
			break
		}
	}
	if found == nil {
		return Event{}, fmt.Errorf("event %s: %w", eventID, ErrNotFound)
	}
	if found.Reason != "" {
		return Event{}, fmt.Errorf("event %s already has a reason", eventID)
	}

	now := s.now()
	updated := *found
	updated.Reason = reason
	updated.ReasonAt = &now

	// The log is append-only: the explanation is a new line that supersedes
	// the original, rather than an edit in place.
	if err := s.appendEvent(updated); err != nil {
		return Event{}, err
	}
	return updated, nil
}

// ---- internals ----

func (s *Store) now() time.Time { return time.Now().In(s.loc) }

func (s *Store) today() time.Time {
	n := s.now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, s.loc)
}

func (s *Store) snapshot() []Task {
	out := make([]Task, len(s.st.Tasks))
	copy(out, s.st.Tasks)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func (s *Store) indexOf(id string) (int, error) {
	for i := range s.st.Tasks {
		if s.st.Tasks[i].ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("task %s: %w", id, ErrNotFound)
}

func (s *Store) mustGet(id string) Task {
	i, err := s.indexOf(id)
	if err != nil {
		return Task{}
	}
	return s.st.Tasks[i]
}

// openIndexes lists tasks still in the queue, in queue order.
func (s *Store) openIndexes() []int {
	return s.indexesWithState(StateOpen)
}

func (s *Store) waitingIndexes() []int {
	return s.indexesWithState(StateWaiting)
}

func (s *Store) indexesWithState(st State) []int {
	var idx []int
	for i := range s.st.Tasks {
		if s.st.Tasks[i].State == st {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return s.st.Tasks[idx[a]].Order < s.st.Tasks[idx[b]].Order
	})
	return idx
}

// move places the task at storeIdx into position to among the open tasks.
func (s *Store) move(open []int, storeIdx, to int) {
	ordered := make([]int, 0, len(open))
	for _, i := range open {
		if i != storeIdx {
			ordered = append(ordered, i)
		}
	}
	if to > len(ordered) {
		to = len(ordered)
	}
	ordered = append(ordered[:to], append([]int{storeIdx}, ordered[to:]...)...)
	for pos, i := range ordered {
		s.st.Tasks[i].Order = pos
	}
}

// renumber gives the queue positions 0..n-1 and pushes waiting and done tasks
// past it, so Order alone sorts the whole list the way the page shows it.
func (s *Store) renumber() {
	pos := 0
	for _, i := range s.openIndexes() {
		s.st.Tasks[i].Order = pos
		pos++
	}
	for _, i := range s.waitingIndexes() {
		s.st.Tasks[i].Order = pos
		pos++
	}
	for _, i := range s.indexesWithState(StateDone) {
		s.st.Tasks[i].Order = pos
		pos++
	}
}

// record stamps an event with an ID and time, then appends it.
func (s *Store) record(e Event) error {
	e.ID = fmt.Sprintf("E%04d", s.st.NextEvent)
	s.st.NextEvent++
	e.At = s.now()
	return s.appendEvent(e)
}

func (s *Store) appendEvent(e Event) error {
	path := s.eventsPath(e.At)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() // write errors are reported below

	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding event: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("appending to %s: %w", path, err)
	}
	return nil
}

// readEvents reads every month's log in order. A later line for the same event
// ID supersedes an earlier one, which is how Why records an explanation
// without editing history.
func (s *Store) readEvents() ([]Event, error) {
	matches, err := filepath.Glob(filepath.Join(s.dir, "events-*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("listing event logs: %w", err)
	}
	sort.Strings(matches)

	byID := make(map[string]Event)
	var order []string
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", path, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for sc.Scan() {
			line++
			raw := sc.Bytes()
			if len(raw) == 0 {
				continue
			}
			var e Event
			if err := json.Unmarshal(raw, &e); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("parsing %s line %d: %w", path, line, err)
			}
			if _, seen := byID[e.ID]; !seen {
				order = append(order, e.ID)
			}
			byID[e.ID] = e
		}
		if err := sc.Err(); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		_ = f.Close()
	}

	out := make([]Event, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// persist rewrites tasks.json. Every write goes through here, so this is also
// where the revision moves on.
func (s *Store) persist() error {
	s.st.Revision++

	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, "tasks-*.json")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting permissions on temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmpName, s.statePath()); err != nil {
		return fmt.Errorf("replacing %s: %w", s.statePath(), err)
	}
	return nil
}

func lastEventOfKind(all []Event, taskID string, kind Kind) (Event, bool) {
	var found Event
	var ok bool
	for _, e := range all {
		if e.TaskID == taskID && e.Kind == kind {
			found, ok = e, true
		}
	}
	return found, ok
}

// dueWithin reports whether a due date falls on or before today+days, which
// includes anything already overdue. An empty due date is never within scope.
func dueWithin(due string, today time.Time, days int, loc *time.Location) (bool, int) {
	if due == "" {
		return false, 0
	}
	d, err := ParseDate(due, loc)
	if err != nil {
		return false, 0
	}
	diff := int(d.Sub(today).Hours() / 24)
	return diff <= days, diff
}
