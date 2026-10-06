package note

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// clock returns a time source that moves one minute per call, starting in
// October 2026, so ordering and the monthly log file are deterministic.
func clock() func() time.Time {
	t := time.Date(2026, 10, 6, 16, 45, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(time.Minute)
		return t
	}
}

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, clock())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, dir
}

func mustAdd(t *testing.T, s *Store, text, topic string) Note {
	t.Helper()
	n, err := s.Add(text, topic)
	if err != nil {
		t.Fatalf("Add(%q): %v", text, err)
	}
	return n
}

func ids(notes []Note) []int {
	out := make([]int, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.ID)
	}
	return out
}

func TestAddExtractsURLAndTrims(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "  https://x.slack.com/a/1 デプロイ変わった \n", "")
	if n.ID != 1 || n.Text != "https://x.slack.com/a/1 デプロイ変わった" || n.URL != "https://x.slack.com/a/1" {
		t.Errorf("unexpected note %+v", n)
	}
	if !n.Pending() || n.Annotations == nil {
		t.Errorf("new note should be pending with a non-nil annotations list: %+v", n)
	}
	if got := mustAdd(t, s, "リンクなし", "").URL; got != "" {
		t.Errorf("URL = %q, want empty", got)
	}
}

func TestAddRejectsEmptyText(t *testing.T) {
	s, _ := openStore(t)
	for _, text := range []string{"", "   ", "\n\t"} {
		_, err := s.Add(text, "")
		var inv InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("Add(%q) err = %v, want InvalidError", text, err)
		}
	}
	if len(s.List(Filter{})) != 0 {
		t.Error("a rejected add must not store anything")
	}
}

func TestAddRegistersTopic(t *testing.T) {
	s, _ := openStore(t)
	mustAdd(t, s, "a", " 青分離 ")
	if got := s.Topics(); !reflect.DeepEqual(got, []string{"青分離"}) {
		t.Errorf("Topics = %v", got)
	}
	mustAdd(t, s, "b", "青分離")
	if got := s.Topics(); len(got) != 1 {
		t.Errorf("same topic twice should stay one: %v", got)
	}
}

func TestListFilters(t *testing.T) {
	s, _ := openStore(t)
	a := mustAdd(t, s, "a", "X")
	b := mustAdd(t, s, "b", "Y")
	c := mustAdd(t, s, "c", "")
	d := mustAdd(t, s, "d", "X")
	if _, err := s.Annotate(a.ID, AnnotationInput{Kind: KindMemo, Body: "x"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		f    Filter
		want []int
	}{
		{"all newest first", Filter{}, []int{d.ID, c.ID, b.ID, a.ID}},
		{"topic", Filter{Topic: "X"}, []int{d.ID, a.ID}},
		{"pending", Filter{Pending: true}, []int{d.ID, c.ID, b.ID}},
		{"no topic", Filter{NoTopic: true}, []int{c.ID}},
		{"topic and pending", Filter{Topic: "X", Pending: true}, []int{d.ID}},
		{"unknown topic", Filter{Topic: "Z"}, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ids(s.List(tt.f)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("List = %v, want %v", got, tt.want)
			}
		})
	}
	if got := s.PendingCount(); got != 3 {
		t.Errorf("PendingCount = %d, want 3", got)
	}
}

func TestAnnotateAppendsOnly(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "a", "")
	first, err := s.Annotate(n.ID, AnnotationInput{Kind: KindQuote, Body: "引用", Source: " slack "})
	if err != nil {
		t.Fatal(err)
	}
	if first.Author != AuthorAI || first.Source != "slack" || first.ID != 1 {
		t.Errorf("defaults wrong: %+v", first)
	}
	second, err := s.Annotate(n.ID, AnnotationInput{Kind: KindAnalysis, Body: "考察", Author: AuthorHuman})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != 2 || second.Author != AuthorHuman {
		t.Errorf("second annotation wrong: %+v", second)
	}

	got, _ := s.Get(n.ID)
	if len(got.Annotations) != 2 || got.Annotations[0] != first || got.Annotations[1] != second {
		t.Errorf("annotations should be both, in order: %+v", got.Annotations)
	}
	if got.Pending() {
		t.Error("annotated note must not be pending")
	}
}

func TestGetResultIsNotAliased(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "a", "")
	before, _ := s.Get(n.ID)
	if _, err := s.Annotate(n.ID, AnnotationInput{Kind: KindMemo, Body: "later"}); err != nil {
		t.Fatal(err)
	}
	if len(before.Annotations) != 0 {
		t.Error("a note read earlier must not change when annotations are appended")
	}
}

func TestAnnotateRejects(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "a", "")
	tests := []struct {
		want error // nil means InvalidError
		in   AnnotationInput
		name string
		id   int
	}{
		{nil, AnnotationInput{Kind: "essay", Body: "x"}, "unknown kind", n.ID},
		{nil, AnnotationInput{Body: "x"}, "empty kind", n.ID},
		{nil, AnnotationInput{Kind: KindMemo, Body: "  \n"}, "empty body", n.ID},
		{nil, AnnotationInput{Kind: KindMemo, Body: "x", Author: "bot"}, "unknown author", n.ID},
		{ErrNotFound, AnnotationInput{Kind: KindMemo, Body: "x"}, "unknown id", 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Annotate(tt.id, tt.in)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Errorf("err = %v, want %v", err, tt.want)
				}
				return
			}
			var inv InvalidError
			if !errors.As(err, &inv) {
				t.Errorf("err = %v, want InvalidError", err)
			}
		})
	}
	if got, _ := s.Get(n.ID); len(got.Annotations) != 0 {
		t.Error("rejected annotations must not be stored")
	}
}

func TestSetTopicAndClear(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "a", "")
	got, err := s.SetTopic(n.ID, " C&C ")
	if err != nil || got.Topic != "C&C" {
		t.Fatalf("SetTopic = %+v, %v", got, err)
	}
	if topics := s.Topics(); !reflect.DeepEqual(topics, []string{"C&C"}) {
		t.Errorf("topic should be registered: %v", topics)
	}
	got, err = s.SetTopic(n.ID, "")
	if err != nil || got.Topic != "" {
		t.Fatalf("clear = %+v, %v", got, err)
	}
	if topics := s.Topics(); len(topics) != 1 {
		t.Errorf("clearing a memo's topic keeps the registered topic: %v", topics)
	}
	if _, err := s.SetTopic(99, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id err = %v", err)
	}
}

func TestDelete(t *testing.T) {
	s, _ := openStore(t)
	a := mustAdd(t, s, "a", "")
	b := mustAdd(t, s, "b", "")
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := ids(s.List(Filter{})); !reflect.DeepEqual(got, []int{b.ID}) {
		t.Errorf("List after delete = %v", got)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get deleted err = %v", err)
	}
	if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete err = %v", err)
	}
	if c := mustAdd(t, s, "c", ""); c.ID != 3 {
		t.Errorf("ids must not be reused, got %d", c.ID)
	}
}

func TestTopics(t *testing.T) {
	s, _ := openStore(t)
	if got := s.Topics(); got == nil || len(got) != 0 {
		t.Errorf("empty store Topics = %#v, want empty non-nil", got)
	}
	if err := s.AddTopic("  青分離  "); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTopic("青分離"); err == nil {
		t.Error("duplicate topic should be rejected")
	}
	for _, name := range []string{"", "   "} {
		var inv InvalidError
		if err := s.AddTopic(name); !errors.As(err, &inv) {
			t.Errorf("AddTopic(%q) err = %v, want InvalidError", name, err)
		}
	}
	mustAdd(t, s, "a", "C&C") // already registered through use
	if err := s.AddTopic("C&C"); err == nil {
		t.Error("a topic already on a memo counts as existing")
	}
	if got := s.Topics(); !reflect.DeepEqual(got, []string{"青分離", "C&C"}) {
		t.Errorf("Topics = %v", got)
	}
}

func TestTopicsIncludesUsedButUnregistered(t *testing.T) {
	dir := t.TempDir()
	// notes.json from something that did not keep the topic list.
	body := `{"notes":[{"id":1,"text":"a","url":"","topic":"古い話題","created_at":"2026-10-01T00:00:00Z","annotations":[]}],"topics":[],"next_note":2,"next_annotation":1}`
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Topics(); !reflect.DeepEqual(got, []string{"古い話題"}) {
		t.Errorf("Topics = %v", got)
	}
}

func TestReopenRestoresState(t *testing.T) {
	s, dir := openStore(t)
	n := mustAdd(t, s, "https://x.notion.so/p 決める", "青分離")
	if _, err := s.Annotate(n.ID, AnnotationInput{Kind: KindAnalysis, Body: "考察\n二行目"}); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, "b", "")
	if err := s.AddTopic("C&C"); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.List(Filter{}), s.List(Filter{})) {
		t.Errorf("notes differ after reopen:\n got %+v\nwant %+v", again.List(Filter{}), s.List(Filter{}))
	}
	if !reflect.DeepEqual(again.Topics(), s.Topics()) {
		t.Errorf("topics differ after reopen: %v vs %v", again.Topics(), s.Topics())
	}
	if c, _ := again.Add("c", ""); c.ID != 3 {
		t.Errorf("next id after reopen = %d, want 3", c.ID)
	}
	if a, _ := again.Annotate(n.ID, AnnotationInput{Kind: KindMemo, Body: "x"}); a.ID != 2 {
		t.Errorf("next annotation id after reopen = %d, want 2", a.ID)
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil {
		t.Error("Open should fail on a corrupt notes.json instead of starting empty")
	}

	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "sub"), nil); err == nil {
		t.Error("Open should fail when the data dir cannot be created")
	}

	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, "notes.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(unreadable, nil); err == nil {
		t.Error("Open should fail when notes.json cannot be read")
	}
}

func TestEventsAreAppended(t *testing.T) {
	s, dir := openStore(t)
	n := mustAdd(t, s, "a", "T")
	if _, err := s.Annotate(n.ID, AnnotationInput{Kind: KindMemo, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetTopic(n.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTopic("U"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(n.ID); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, "events-2026-10.jsonl"))
	if err != nil {
		t.Fatalf("monthly log missing: %v", err)
	}
	defer func() { _ = f.Close() }()
	var ops []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad log line %q: %v", sc.Text(), err)
		}
		ops = append(ops, ev.Op)
	}
	want := []string{"add_note", "annotate", "set_topic", "add_topic", "delete_note"}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("logged ops = %v, want %v", ops, want)
	}
}

func TestRejectedCallsAreNotLogged(t *testing.T) {
	s, dir := openStore(t)
	_, _ = s.Add("", "")
	_ = s.Delete(5)
	if entries, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl")); len(entries) != 0 {
		t.Errorf("rejected calls must not write a log: %v", entries)
	}
}

func TestStateFileIsReplacedAtomically(t *testing.T) {
	s, dir := openStore(t)
	mustAdd(t, s, "a", "")
	mustAdd(t, s, "b", "")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.json")); err != nil {
		t.Errorf("notes.json missing: %v", err)
	}
}

func TestWriteFailureKeepsOldState(t *testing.T) {
	s, dir := openStore(t)
	mustAdd(t, s, "a", "")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }() // so TempDir cleanup works
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		_ = f.Close()
		t.Skip("directory permissions are not enforced here (running as root?)")
	}
	if _, err := s.Add("b", ""); err == nil {
		t.Fatal("Add should fail when the state file cannot be written")
	}
	if got := ids(s.List(Filter{})); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("a failed write must leave the old state, got %v", got)
	}
}

func TestEventWriteFailureStillSucceeds(t *testing.T) {
	s, dir := openStore(t)
	// A directory where the monthly log should be makes appending fail.
	if err := os.Mkdir(filepath.Join(dir, "events-2026-10.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	n, err := s.Add("a", "")
	if err != nil {
		t.Fatalf("a failed log append must not fail the call (the caller would retry and duplicate): %v", err)
	}
	if got, gerr := s.Get(n.ID); gerr != nil || got.Text != "a" {
		t.Errorf("state should have changed: %+v, %v", got, gerr)
	}
	again, err := Open(dir, nil)
	if err == nil {
		t.Fatal("store is still open; second Open must fail")
	}
	_ = again
	_ = s.Close()
	again, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.List(Filter{})) != 1 {
		t.Error("the saved state should be on disk")
	}
}

func TestSecondOpenOnSameDirFails(t *testing.T) {
	s, dir := openStore(t)
	second, err := Open(dir, nil)
	if err == nil || second != nil {
		t.Fatalf("second Open should fail, got %v, %v", second, err)
	}
	if !strings.Contains(err.Error(), "別のサーバが使っています") {
		t.Errorf("err = %v", err)
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if cerr := s.Close(); cerr != nil {
		t.Errorf("Close twice should be harmless: %v", cerr)
	}
	third, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open after Close should work: %v", err)
	}
	_ = third.Close()
}

func TestOpenFailureReleasesLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil {
		t.Fatal("corrupt file should fail")
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte(`{"notes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("a failed Open must not leave the directory locked: %v", err)
	}
	_ = s.Close()
}
