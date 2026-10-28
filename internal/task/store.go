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
	"strings"
	"sync"
	"time"

	"github.com/rengotaku/workbench/internal/store"
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
	Tasks     []Task   `json:"tasks"`
	Undone    []string `json:"undone,omitempty"`
	NextTask  int      `json:"next_task"`
	NextEvent int      `json:"next_event"`
	Revision  int      `json:"revision"`
}

// Store keeps the current tasks in tasks.json and every change in
// events-YYYY-MM.jsonl beside it. The state file is rewritten atomically; the
// event log is only ever appended to.
type Store struct {
	lock *store.Lock // holds the data dir's single-writer lock for the Store's lifetime
	loc  *time.Location
	dir  string
	st   state
	mu   sync.Mutex
}

// Open loads a store from dir, creating an empty one when nothing is there.
func Open(dir string, loc *time.Location) (*Store, error) {
	if loc == nil {
		loc = time.Local
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	lock, err := store.AcquireLock(dir)
	switch {
	case errors.Is(err, store.ErrLocked):
		return nil, fmt.Errorf("data directory %s is in use by another process; stop it or set a different DATA_DIR", dir)
	case err != nil:
		return nil, fmt.Errorf("locking %s: %w", dir, err)
	}
	s := &Store{dir: dir, loc: loc, lock: lock, st: state{NextTask: 1, NextEvent: 1}}

	b, err := os.ReadFile(s.statePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		_ = s.Close()
		return nil, fmt.Errorf("reading %s: %w", s.statePath(), err)
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("parsing %s: %w", s.statePath(), err)
	}
	if s.st.NextTask < 1 {
		s.st.NextTask = 1
	}
	if s.st.NextEvent < 1 {
		s.st.NextEvent = 1
	}
	// Data written before ADR todo-0012 can have a focused task below an
	// unfocused one. Normalising in memory is enough: the next write saves it.
	s.renumber()
	return s, nil
}

// Close releases the data directory lock. The Store must not be used after.
func (s *Store) Close() error {
	if s.lock == nil {
		return nil
	}
	err := s.lock.Close()
	s.lock = nil
	return err
}

func (s *Store) statePath() string { return filepath.Join(s.dir, "tasks.json") }

func (s *Store) eventsPath(at time.Time) string {
	return store.MonthlyLogPath(s.dir, at.In(s.loc))
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
	return s.AddLinked(name, detail, due, "", "", actor)
}

// AddLinked appends a task that already carries the link it was pasted with.
// Adding and linking are one change, so a single undo takes the task back out
// instead of leaving it behind without its link.
func (s *Store) AddLinked(name, detail, due, url, linkText string, actor Actor) (Task, error) {
	return s.add(name, detail, due, url, linkText, nil, 0, "", actor)
}

// AddWithLinks appends a task together with its related URLs, numbered from 1
// in the order given. Adding and linking are one event, so one undo removes
// the task.
func (s *Store) AddWithLinks(name, detail, due string, links []LinkInput, actor Actor) (Task, error) {
	return s.add(name, detail, due, "", "", links, 0, "", actor)
}

// AddFromNote appends a task made from a memo. When a task for that memo
// already exists it is returned unchanged and nothing is created, so a retry
// after a failure further along cannot make a second one.
func (s *Store) AddFromNote(name, detail, due string, noteID int, actor Actor) (Task, error) {
	if noteID < 1 {
		return Task{}, errors.New("source note id is required")
	}
	return s.add(name, detail, due, "", "", nil, noteID, "", actor)
}

// AddChild appends a task cut out of another task (ADR todo-0019, todo-0020).
// Checking the parent, adding and tying are one change under one lock: a
// parent that is finished meanwhile refuses the add rather than leaving the
// task behind untied, and one undo takes the task back out. Under an open
// parent the child goes directly above it, in the same band.
func (s *Store) AddChild(name, detail, due, parent string, links []LinkInput, url, linkText string, actor Actor) (Task, error) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return Task{}, errors.New("parent is required")
	}
	return s.add(name, detail, due, url, linkText, links, 0, parent, actor)
}

// FindBySourceNote returns the task made from a memo.
func (s *Store) FindBySourceNote(noteID int) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findBySourceNote(noteID)
}

func (s *Store) findBySourceNote(noteID int) (Task, bool) {
	if noteID < 1 {
		return Task{}, false
	}
	for _, t := range s.st.Tasks {
		if t.SourceNoteID == noteID {
			return t, true
		}
	}
	return Task{}, false
}

func (s *Store) add(name, detail, due, url, linkText string, linkInputs []LinkInput, noteID int, parent string, actor Actor) (Task, error) {
	if name == "" {
		return Task{}, errors.New("name is required")
	}
	if due != "" {
		if _, err := ParseDate(due, s.loc); err != nil {
			return Task{}, err
		}
	}

	links, err := buildLinks(linkInputs)
	if err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.checkParent(Task{Due: due}, parent); err != nil {
		return Task{}, err
	}
	if existing, ok := s.findBySourceNote(noteID); ok {
		// It may be in memory only, if saving it failed the first time. Save
		// again before saying it exists, so the caller never records a task
		// that is not on disk.
		if err := s.persist(); err != nil {
			return Task{}, err
		}
		return existing, nil
	}
	t := Task{
		ID:           fmt.Sprintf("T%03d", s.st.NextTask),
		Name:         name,
		Detail:       detail,
		Due:          due,
		URL:          url,
		Links:        links,
		State:        StateOpen,
		Order:        len(s.openIndexes()),
		CreatedAt:    s.now(),
		SourceNoteID: noteID,
		Parent:       parent,
	}
	if url != "" {
		t.LinkText = linkText
	}
	var parentOrder = -1
	if parent != "" {
		if p := s.st.Tasks[s.mustIndex(parent)]; p.State == StateOpen {
			t.Focused = p.Focused
			parentOrder = p.Order
		}
	}
	s.st.NextTask++
	s.st.Tasks = append(s.st.Tasks, t)
	if parentOrder >= 0 {
		s.move(s.openIndexes(), len(s.st.Tasks)-1, parentOrder)
	}
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
	ev := Event{TaskID: id, Kind: KindDone, Actor: actor}
	if t.State == StateLong {
		ev.FromValue = string(StateLong) // undo puts it back in the long-run band
	}
	t.State = StateDone
	t.CompletedAt = &now
	t.Focused = false // finishing it takes it out of the focus band
	s.st.Tasks[i] = t
	s.renumber()

	if err := s.record(ev); err != nil {
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
	if t.State == StateLong {
		return Task{}, fmt.Errorf("%s is long-run: bring it back to the queue before handing it on", id)
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

// Resume brings a waiting or someday task back into the queue, at the end of
// it. Clearing the handoff means a future chase starts its own count; clearing
// Until means a future shelve starts its own.
func (s *Store) Resume(id, note string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.State != StateWaiting && t.State != StateSomeday {
		return Task{}, fmt.Errorf("%s is not waiting or someday", id)
	}

	from := t.HandedTo
	t.State = StateOpen
	t.HandedTo = ""
	t.HandedOn = ""
	t.WaitNote = ""
	t.Until = ""
	t.Order = math.MaxInt32 // renumber puts it at the end of the queue
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

// Shelve moves an open or waiting task to someday: worth doing, not now.
// Unlike Wait, nobody else holds it — until is a date worth revisiting, not a
// promise, so it shares neither colour nor count with Due. Passing an empty
// until sets it to a month out.
//
// Calling it again on a task already someday extends it in place: the state
// and its place in Order do not move, only Until changes. Triage otherwise
// needed a two-step detour — resume, then shelve again — to push a date back,
// and that detour put the task through the queue for an instant.
func (s *Store) Shelve(id, until string, actor Actor) (Task, error) {
	if until != "" {
		if _, err := ParseDate(until, s.loc); err != nil {
			return Task{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	before := s.st.Tasks[i]
	if before.State != StateOpen && before.State != StateWaiting && before.State != StateSomeday {
		return Task{}, fmt.Errorf("%s is %s: cannot shelve", id, before.State)
	}

	if until == "" {
		until = s.now().AddDate(0, 1, 0).Format(DateLayout)
	}
	t := before
	t.State = StateSomeday
	t.Until = until
	t.Focused = false // shelved out of the queue does not stay in the band
	s.st.Tasks[i] = t
	if before.State != StateSomeday {
		s.renumber() // an extension keeps its place; a fresh shelve moves bands
	}

	snapshot := before
	ev := Event{TaskID: id, Kind: KindShelved, Actor: actor, FromValue: before.Until, ToValue: until, Snapshot: &snapshot}
	if err := s.record(ev); err != nil {
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
//
// reason may be empty: then the removal goes to the question queue like
// before. The screen asks for one up front when the task has been in the list
// for a while (ADR todo-0016); the CLI and the API leave it optional.
func (s *Store) Remove(id string, actor Actor, reason string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	removed := s.st.Tasks[i]

	s.st.Tasks = append(s.st.Tasks[:i], s.st.Tasks[i+1:]...)
	s.renumber()

	snapshot := removed
	ev := Event{TaskID: id, Kind: KindRemoved, Actor: actor, ToValue: removed.Name, Snapshot: &snapshot,
		Reason: strings.TrimSpace(reason)}
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

// SetFocus lifts a task into the focus band, or takes it out. Focused tasks
// are the top of the queue (ADR todo-0012): focusing moves the task to the end
// of the focused block, unfocusing puts it just below that block.
func (s *Store) SetFocus(id string, on bool, actor Actor) (Task, error) {
	return s.FocusAt(id, on, -1, 0, actor)
}

// FocusAt is SetFocus with a place: to is the queue position the task was
// dropped at, clamped to the block it belongs to once focus has changed. A
// negative to means the edge of the focused block, as SetFocus does. Tasks
// outside the queue (waiting) only change their mark.
//
// expectRevision guards a drop the same way it guards Reorder: pass the
// revision the positions were read at, or 0 to skip the check.
func (s *Store) FocusAt(id string, on bool, to, expectRevision int, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if expectRevision != 0 && expectRevision != s.st.Revision {
		return Task{}, fmt.Errorf("%w (read at %d, now %d)", ErrStale, expectRevision, s.st.Revision)
	}
	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if t.Focused == on {
		return t, nil
	}
	if on && t.State == StateDone {
		return Task{}, fmt.Errorf("%s is done", id)
	}
	if on && t.State == StateLong {
		return Task{}, fmt.Errorf("%s is long-run: it has its own band", id)
	}
	from := t.Order
	backup := append([]Task(nil), s.st.Tasks...)
	t.Focused = on
	s.st.Tasks[i] = t
	if t.State == StateOpen {
		open := s.openIndexes()
		s.move(open, i, s.clampToBlock(open, i, to))
		if c, p, bad := s.orderViolation(); bad {
			// A place the caller named is refused; with none named (the
			// button) the task goes to the nearest place that keeps the rule.
			if to >= 0 || !s.placeByRule(i) {
				s.st.Tasks = backup
				return Task{}, orderRule(c, p)
			}
		}
		s.renumber()
	}

	kind := KindUnfocused
	if on {
		kind = KindFocused
	}
	ev := Event{TaskID: id, Kind: kind, Actor: actor}
	if t.State == StateOpen {
		ev.FromOrder, ev.ToOrder = from, s.st.Tasks[i].Order
	}
	if err := s.record(ev); err != nil {
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
	if err := s.checkDue(t); err != nil {
		return Task{}, err
	}
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
	from := t.Detail
	t.Detail = detail
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindDetail, Actor: actor, FromValue: from, ToValue: detail}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// Limits on one annotation, in bytes. The event log is read line by line with
// a 1 MiB limit per line; an annotation is logged with its body, so it must
// stay far below that or every later read of the log fails.
const (
	MaxAnnotationBody   = 64 * 1024
	MaxAnnotationSource = 200
)

// AnnotationInput is what a caller supplies; the store assigns id and time.
type AnnotationInput struct {
	Kind   string
	Body   string
	Source string
}

// Annotate appends a supplement to a task. It is recorded as an event but is
// never undoable, and existing annotations are never touched.
func (s *Store) Annotate(id string, in AnnotationInput, actor Actor) (Annotation, error) {
	switch in.Kind {
	case AnnotationQuote, AnnotationAnalysis, AnnotationMemo, AnnotationProgress, AnnotationBlocked:
	default:
		return Annotation{}, fmt.Errorf("kind must be quote, analysis, memo, progress or blocked: %q", in.Kind)
	}
	if strings.TrimSpace(in.Body) == "" {
		return Annotation{}, errors.New("body is required")
	}
	if len(in.Body) > MaxAnnotationBody {
		return Annotation{}, fmt.Errorf("body is too long (%d bytes, limit %d); split it into several annotations", len(in.Body), MaxAnnotationBody)
	}
	if len(strings.TrimSpace(in.Source)) > MaxAnnotationSource {
		return Annotation{}, fmt.Errorf("source is too long (limit %d bytes)", MaxAnnotationSource)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Annotation{}, err
	}
	t := s.st.Tasks[i]
	a := Annotation{
		ID:        len(t.Annotations) + 1,
		Kind:      in.Kind,
		Body:      in.Body,
		Source:    strings.TrimSpace(in.Source),
		Author:    actor,
		CreatedAt: s.now(),
	}
	t.Annotations = append(append([]Annotation(nil), t.Annotations...), a)
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindAnnotated, Actor: actor, FromValue: in.Kind, ToValue: in.Body}); err != nil {
		return Annotation{}, err
	}
	if err := s.persist(); err != nil {
		return Annotation{}, err
	}
	return a, nil
}

// SetLink records the link a task came from: the URL, and the part of the name
// that was the link at the source. An empty url clears both. The values are
// stored as given — the store never builds a URL or picks a range of its own.
func (s *Store) SetLink(id, url, linkText string, actor Actor) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	from := t.URL
	t.URL = url
	t.LinkText = linkText
	if url == "" {
		t.LinkText = ""
	}
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindLinked, Actor: actor, FromValue: from, ToValue: url}); err != nil {
		return Task{}, err
	}
	if err := s.persist(); err != nil {
		return Task{}, err
	}
	return s.mustGet(id), nil
}

// SetLinks replaces the task's related URLs with links, numbered from 1 by
// position; an empty list removes them all. A list equal to the current one
// records nothing.
func (s *Store) SetLinks(id string, linkInputs []LinkInput, actor Actor) (Task, error) {
	links, err := buildLinks(linkInputs)
	if err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i, err := s.indexOf(id)
	if err != nil {
		return Task{}, err
	}
	t := s.st.Tasks[i]
	if sameLinks(t.Links, links) {
		return t, nil
	}
	from := encodeLinks(t.Links)
	t.Links = links
	s.st.Tasks[i] = t

	if err := s.record(Event{TaskID: id, Kind: KindLinks, Actor: actor, FromValue: from, ToValue: encodeLinks(links)}); err != nil {
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
	// A reorder cannot carry a task across the edge of the focused block;
	// crossing it is a change of focus, which FocusAt does.
	to = s.clampToBlock(open, i, to)

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

	backup := append([]Task(nil), s.st.Tasks...)
	s.move(open, i, to)
	if c, p, bad := s.orderViolation(); bad {
		s.st.Tasks = backup
		return Task{}, orderRule(c, p)
	}
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
	Event   Event  `json:"event"`
	Task    Task   `json:"task"`
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

	undone := s.undoneSet()

	var out []Pending
	for _, e := range all {
		if !e.NeedsReason(queueLen) {
			continue
		}
		// A dropped task is no longer in the list, so its own snapshot is the
		// only place left to read its name from.
		if e.Kind == KindRemoved {
			if undone[e.ID] || e.Snapshot == nil {
				continue // put back again, or too old to carry a snapshot
			}
			out = append(out, Pending{Event: e, Task: *e.Snapshot, Trigger: e.AskReason(queueLen)})
			continue
		}
		i, err := s.indexOf(e.TaskID)
		if err != nil {
			continue // task removed since; nothing to ask about
		}
		out = append(out, Pending{Event: e, Task: s.st.Tasks[i], Trigger: e.AskReason(queueLen)})
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

// Drop is one task taken off the list, kept readable after the task itself is
// gone. The name and due date come from the snapshot the removal wrote.
type Drop struct {
	At      time.Time `json:"at"`
	EventID string    `json:"event_id"`
	TaskID  string    `json:"task_id"`
	Name    string    `json:"name"`
	Due     string    `json:"due,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Actor   Actor     `json:"actor"`
}

// Dropped returns the tasks removed on or after since, most recent first.
//
// Removals that were undone are left out: the task is back on the list, so it
// was never dropped. That also covers the removal an undone "created" writes,
// which is an artefact of undo rather than a decision to stop doing something.
func (s *Store) Dropped(since time.Time) ([]Drop, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readEvents()
	if err != nil {
		return nil, err
	}
	undone := s.undoneSet()

	var out []Drop
	for _, e := range all {
		if e.Kind != KindRemoved || undone[e.ID] || e.At.In(s.loc).Before(since) {
			continue
		}
		d := Drop{At: e.At, EventID: e.ID, TaskID: e.TaskID, Name: e.ToValue, Reason: e.Reason, Actor: e.Actor}
		if e.Snapshot != nil {
			if d.Name == "" {
				d.Name = e.Snapshot.Name
			}
			d.Due = e.Snapshot.Due
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

func (s *Store) undoneSet() map[string]bool {
	m := make(map[string]bool, len(s.st.Undone))
	for _, id := range s.st.Undone {
		m[id] = true
	}
	return m
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

func (s *Store) somedayIndexes() []int {
	return s.indexesWithState(StateSomeday)
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

// clampToBlock fits a queue position for the task at storeIdx into the block
// it belongs to: focused tasks sit at the top of the queue, the rest below
// them. A negative to means the edge of the focused block. open is the queue
// in order, the task itself included.
func (s *Store) clampToBlock(open []int, storeIdx, to int) int {
	focused := 0
	for _, idx := range open {
		if idx != storeIdx && s.st.Tasks[idx].Focused {
			focused++
		}
	}
	lo, hi := focused, len(open)-1
	if s.st.Tasks[storeIdx].Focused {
		lo, hi = 0, focused
	}
	switch {
	case to < 0:
		return focused
	case to < lo:
		return lo
	case to > hi:
		return hi
	}
	return to
}

// renumber gives the queue positions 0..n-1 and pushes long-run, waiting,
// someday and done tasks past it in that order, so Order alone sorts the whole list the
// way the page shows it. Focused tasks come first in the queue, keeping their
// order among themselves (ADR todo-0012), so a resume or an undo cannot leave
// one below an unfocused task. Within a band a child goes above its parent.
func (s *Store) renumber() {
	pos := 0
	open := s.openIndexes()
	for _, focused := range []bool{true, false} {
		var group []int
		for _, i := range open {
			if s.st.Tasks[i].Focused == focused {
				group = append(group, i)
			}
		}
		// A child never sits below its open parent in the same band (ADR
		// todo-0020): resume, reopen and unfocus paths all come through here.
		for _, i := range s.childrenFirst(group) {
			s.st.Tasks[i].Order = pos
			pos++
		}
	}
	for _, i := range s.indexesWithState(StateLong) {
		s.st.Tasks[i].Order = pos
		pos++
	}
	for _, i := range s.waitingIndexes() {
		s.st.Tasks[i].Order = pos
		pos++
	}
	for _, i := range s.somedayIndexes() {
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
	_, err := s.recordID(e)
	return err
}

// recordID is record, returning the ID it assigned. Undo needs it so the
// reversal it writes can be marked as already spent.
func (s *Store) recordID(e Event) (string, error) {
	e.ID = fmt.Sprintf("E%04d", s.st.NextEvent)
	s.st.NextEvent++
	e.At = s.now()
	if err := s.appendEvent(e); err != nil {
		return "", err
	}
	return e.ID, nil
}

func (s *Store) appendEvent(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding event: %w", err)
	}
	return store.AppendLine(s.eventsPath(e.At), b)
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
	return store.WriteFileAtomic(s.statePath(), append(b, '\n'))
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
