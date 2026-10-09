package note

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNotFound is returned for an id that does not exist.
var ErrNotFound = errors.New("メモが見つかりません")

// InvalidError is a rejected input. Its text is written for the person (or
// the AI) who sent it, so callers can pass it on as is.
type InvalidError string

func (e InvalidError) Error() string { return string(e) }

// state is the on-disk shape of notes.json.
type state struct {
	Notes          []Note   `json:"notes"`
	Topics         []string `json:"topics"`
	NextNote       int      `json:"next_note"`
	NextAnnotation int      `json:"next_annotation"`
}

// Event is one line of the append-only log.
type Event struct {
	At     time.Time `json:"at"`
	Detail any       `json:"detail,omitempty"`
	Op     string    `json:"op"`
	NoteID int       `json:"note_id,omitempty"`
}

// Store keeps the current memos in notes.json and every operation in
// events-YYYY-MM.jsonl beside it. notes.json is replaced atomically; the log
// is only appended to. The server is the only writer.
type Store struct {
	lock *os.File // holds the data dir's single-writer lock for the Store's lifetime
	dir  string
	now  func() time.Time
	st   state
	mu   sync.Mutex
}

// Open loads the store from dir, creating an empty one when nothing is there.
// now may be nil (time.Now).
func Open(dir string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%s を作れません: %w", dir, err)
	}
	lock, err := acquireLock(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{dir: dir, now: now, lock: lock, st: state{NextNote: 1, NextAnnotation: 1}}

	b, err := os.ReadFile(s.statePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		_ = s.Close()
		return nil, fmt.Errorf("%s を読めません: %w", s.statePath(), err)
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("%s を解釈できません: %w", s.statePath(), err)
	}
	s.st.NextNote = max(s.st.NextNote, 1)
	s.st.NextAnnotation = max(s.st.NextAnnotation, 1)
	return s, nil
}

// acquireLock takes an exclusive, non-blocking lock on dir/.lock. A second
// writer on the same directory would overwrite notes.json from an old
// snapshot and lose memos, so a second Open must fail instead.
func acquireLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ロックファイルを開けません: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("同じデータディレクトリ（%s）を別のサーバが使っています。そちらを止めるか、DATA_DIR を変えて起動してください", dir)
		}
		return nil, fmt.Errorf("ロックを取れません: %w", err)
	}
	return f, nil
}

// Close releases the data directory lock. The Store must not be used after.
func (s *Store) Close() error {
	if s.lock == nil {
		return nil
	}
	err := s.lock.Close() // closing the descriptor drops the flock
	s.lock = nil
	return err
}

func (s *Store) statePath() string { return filepath.Join(s.dir, "notes.json") }

func (s *Store) eventsPath(at time.Time) string {
	return filepath.Join(s.dir, "events-"+at.Format("2006-01")+".jsonl")
}

// commit writes next to notes.json, makes it current, then appends ev to the
// log. A failed state write leaves memory and disk on the old state. The log
// is secondary: once the state is saved the call succeeded, and a failed
// append is only logged (reporting it would make the caller retry and
// duplicate the change).
func (s *Store) commit(next state, ev Event) error {
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("保存内容を作れません: %w", err)
	}
	if err := writeAtomic(s.statePath(), b); err != nil {
		return err
	}
	s.st = next
	if err := s.appendEvent(ev); err != nil {
		slog.Error("event log append failed; state was saved", "op", ev.Op, "error", err)
	}
	return nil
}

func (s *Store) appendEvent(ev Event) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("操作ログを作れません: %w", err)
	}
	f, err := os.OpenFile(s.eventsPath(ev.At), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("操作ログを開けません: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("操作ログに書けません: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("操作ログを閉じられません: %w", err)
	}
	return nil
}

// writeAtomic writes through a temp file and renames it into place, so a
// reader never sees half a file.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notes-*.tmp")
	if err != nil {
		return fmt.Errorf("一時ファイルを作れません: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("一時ファイルに書けません: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("一時ファイルを同期できません: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("一時ファイルを閉じられません: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("%s に置き換えられません: %w", path, err)
	}
	return nil
}

// withTopic returns topics plus name when it is not already there.
func withTopic(topics []string, name string) []string {
	if name == "" || slices.Contains(topics, name) {
		return topics
	}
	return append(slices.Clone(topics), name)
}

// Add stores a new memo. text is trimmed; an empty one is rejected. A topic
// that is not registered yet is registered. links are numbered from 1 in the
// order given, and every [N] in text must have its link.
func (s *Store) Add(text, topic string, linkInputs ...LinkInput) (Note, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Note{}, InvalidError("メモが空です。1 行以上の文字を入れてください")
	}
	topic = strings.TrimSpace(topic)
	links, err := buildLinks(linkInputs)
	if err != nil {
		return Note{}, err
	}
	if err := checkRefs(text, links); err != nil {
		return Note{}, err
	}
	if len(links) == 0 {
		links = nil // stored without a "links" key, like memos from before links
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	at := s.now()
	n := Note{
		ID:          s.st.NextNote,
		Text:        text,
		URL:         ExtractURL(text),
		Topic:       topic,
		Links:       links,
		CreatedAt:   at,
		Annotations: []Annotation{},
	}
	next := s.st
	next.Notes = append(slices.Clone(s.st.Notes), n)
	next.NextNote++
	next.Topics = withTopic(s.st.Topics, topic)
	if err := s.commit(next, Event{At: at, Op: "add_note", NoteID: n.ID, Detail: n}); err != nil {
		return Note{}, err
	}
	return n, nil
}

// Get returns one memo.
func (s *Store) Get(id int) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return Note{}, ErrNotFound
	}
	return s.st.Notes[i], nil
}

func (s *Store) indexOf(id int) int {
	return slices.IndexFunc(s.st.Notes, func(n Note) bool { return n.ID == id })
}

// List returns the memos that match f, newest first.
func (s *Store) List(f Filter) []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Note, 0, len(s.st.Notes))
	for i := len(s.st.Notes) - 1; i >= 0; i-- {
		if f.match(s.st.Notes[i]) {
			out = append(out, s.st.Notes[i])
		}
	}
	return out
}

// PendingCount is the number of memos with no annotation yet.
func (s *Store) PendingCount() int {
	return len(s.List(Filter{Pending: true}))
}

// Delete removes a memo. It exists for the "undo" right after adding one.
func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	next := s.st
	next.Notes = slices.Delete(slices.Clone(s.st.Notes), i, i+1)
	return s.commit(next, Event{At: s.now(), Op: "delete_note", NoteID: id, Detail: s.st.Notes[i]})
}

// AnnotationInput is what a caller supplies; the store assigns id and time.
type AnnotationInput struct {
	Kind   string
	Body   string
	Source string
	Author string // "" means ai
}

// Annotate appends a supplement to a memo. Existing annotations are never
// touched.
func (s *Store) Annotate(id int, in AnnotationInput) (Annotation, error) {
	switch in.Kind {
	case KindQuote, KindAnalysis, KindMemo:
	default:
		return Annotation{}, InvalidError(fmt.Sprintf("kind は quote / analysis / memo のどれかにしてください（受け取った値: %q）", in.Kind))
	}
	if strings.TrimSpace(in.Body) == "" {
		return Annotation{}, InvalidError("body が空です。補足の本文を入れてください")
	}
	author := in.Author
	if author == "" {
		author = AuthorAI
	}
	if author != AuthorAI && author != AuthorHuman {
		return Annotation{}, InvalidError(fmt.Sprintf("author は ai / human のどちらかにしてください（受け取った値: %q）", author))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return Annotation{}, ErrNotFound
	}

	at := s.now()
	a := Annotation{
		ID:        s.st.NextAnnotation,
		Kind:      in.Kind,
		Body:      in.Body,
		Source:    strings.TrimSpace(in.Source),
		Author:    author,
		CreatedAt: at,
	}
	updated := s.st.Notes[i]
	updated.Annotations = append(slices.Clone(updated.Annotations), a)

	next := s.st
	next.Notes = slices.Clone(s.st.Notes)
	next.Notes[i] = updated
	next.NextAnnotation++
	if err := s.commit(next, Event{At: at, Op: "annotate", NoteID: id, Detail: a}); err != nil {
		return Annotation{}, err
	}
	return a, nil
}

// SetTopic sets a memo's topic; "" clears it. A topic that is not registered
// yet is registered.
func (s *Store) SetTopic(id int, topic string) (Note, error) {
	topic = strings.TrimSpace(topic)

	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return Note{}, ErrNotFound
	}
	updated := s.st.Notes[i]
	updated.Topic = topic

	next := s.st
	next.Notes = slices.Clone(s.st.Notes)
	next.Notes[i] = updated
	next.Topics = withTopic(s.st.Topics, topic)
	ev := Event{At: s.now(), Op: "set_topic", NoteID: id, Detail: map[string]string{"from": s.st.Notes[i].Topic, "to": topic}}
	if err := s.commit(next, ev); err != nil {
		return Note{}, err
	}
	return updated, nil
}

// Topics lists every topic: the registered ones in the order they were added,
// then any that only appear on memos.
func (s *Store) Topics() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topicsLocked()
}

func (s *Store) topicsLocked() []string {
	out := slices.Clone(s.st.Topics)
	if out == nil {
		out = []string{}
	}
	for _, n := range s.st.Notes {
		if n.Topic != "" && !slices.Contains(out, n.Topic) {
			out = append(out, n.Topic)
		}
	}
	return out
}

// AddTopic registers a topic name. Empty and duplicate names are rejected.
func (s *Store) AddTopic(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return InvalidError("話題の名前が空です。名前を入れてください")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.topicsLocked(), name) {
		return InvalidError(fmt.Sprintf("話題「%s」はすでにあります", name))
	}
	next := s.st
	next.Topics = withTopic(s.st.Topics, name)
	return s.commit(next, Event{At: s.now(), Op: "add_topic", Detail: name})
}
