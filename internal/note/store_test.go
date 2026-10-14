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
	if _, err := s.SetRoute(a.ID, Route{To: RouteRecord}); err != nil {
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
	if !got.Pending() {
		t.Error("annotating alone must not mark the note as picked up")
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
	if _, err := s.SetRoute(n.ID, Route{To: RouteRecord}); err != nil {
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

func TestSetRouteIsIdempotentAndRejectsAnotherDestination(t *testing.T) {
	s, _ := openStore(t)
	n := mustAdd(t, s, "a", "")

	got, err := s.SetRoute(n.ID, Route{To: RouteTodo, TaskID: "T001"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending() || got.Route == nil || got.Route.TaskID != "T001" {
		t.Fatalf("route not recorded: %+v", got)
	}
	first := *got.TriagedAt

	again, err := s.SetRoute(n.ID, Route{To: RouteTodo, TaskID: "T001"})
	if err != nil || !again.TriagedAt.Equal(first) {
		t.Errorf("same destination should return the current state: %+v %v", again, err)
	}
	if _, err := s.SetRoute(n.ID, Route{To: RouteRecord}); !errors.Is(err, ErrRouteConflict) {
		t.Errorf("err = %v, want ErrRouteConflict", err)
	}
	if _, err := s.SetRoute(n.ID, Route{To: "x"}); err == nil {
		t.Error("unknown destination must be rejected")
	}
	if _, err := s.SetRoute(99, Route{To: RouteRecord}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if s.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", s.PendingCount())
	}
}

func TestOpenMigratesAnnotatedNotesToTriagedOnce(t *testing.T) {
	s, dir := openStore(t)
	a := mustAdd(t, s, "annotated", "")
	b := mustAdd(t, s, "bare", "")
	an, err := s.Annotate(a.ID, AnnotationInput{Kind: KindMemo, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := s.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	makeOldFormat(t, dir)

	s2, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	ga, _ := s2.Get(a.ID)
	if ga.Pending() || ga.Route == nil || ga.Route.To != RouteRecord || !ga.TriagedAt.Equal(an.CreatedAt) {
		t.Errorf("annotated note not migrated: %+v", ga)
	}
	if gb, _ := s2.Get(b.ID); !gb.Pending() {
		t.Errorf("bare note must stay pending: %+v", gb)
	}
	if closeErr := s2.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	before, err := os.ReadFile(filepath.Join(dir, "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	logBefore := countMigrateEvents(t, dir)
	if logBefore != 1 {
		t.Fatalf("migrate events = %d, want 1", logBefore)
	}

	s3, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s3.Close() }()
	after, _ := os.ReadFile(filepath.Join(dir, "notes.json"))
	if string(before) != string(after) {
		t.Error("second start must not change notes.json")
	}
	if got := countMigrateEvents(t, dir); got != logBefore {
		t.Errorf("second start wrote %d more migrate events", got-logBefore)
	}
}

// makeOldFormat rewrites notes.json as a version of the app before triage
// wrote it: no schema_version.
func makeOldFormat(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "notes.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if uerr := json.Unmarshal(b, &m); uerr != nil {
		t.Fatal(uerr)
	}
	if _, ok := m["schema_version"]; !ok {
		t.Fatal("a new store should have written schema_version")
	}
	delete(m, "schema_version")
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A memo annotated after the migration has run is not settled by a restart.
func TestRestartDoesNotSettleANoteAnnotatedAfterMigration(t *testing.T) {
	s, dir := openStore(t)
	old := mustAdd(t, s, "old", "")
	if _, err := s.Annotate(old.ID, AnnotationInput{Kind: KindMemo, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	makeOldFormat(t, dir)

	s2, err := Open(dir, clock()) // the one migration
	if err != nil {
		t.Fatal(err)
	}
	fresh := mustAdd(t, s2, "fresh", "")
	if _, aerr := s2.Annotate(fresh.ID, AnnotationInput{Kind: KindMemo, Body: "y"}); aerr != nil {
		t.Fatal(aerr)
	}
	if cerr := s2.Close(); cerr != nil {
		t.Fatal(cerr)
	}

	s3, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s3.Close() }()
	if g, _ := s3.Get(fresh.ID); !g.Pending() || g.Route != nil {
		t.Errorf("annotated but not routed must stay pending after a restart: %+v", g)
	}
	if g, _ := s3.Get(old.ID); g.Pending() || g.Route.To != RouteRecord {
		t.Errorf("old note should have been recorded once: %+v", g)
	}
	if got := countMigrateEvents(t, dir); got != 1 {
		t.Errorf("migrate events = %d, want 1", got)
	}
}

func countMigrateEvents(t *testing.T, dir string) int {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	n := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n += strings.Count(string(b), `"op":"migrate_triage"`)
	}
	return n
}

func topicNames(s *Store) []string { return s.Topics() }

func TestTopicStatsCountsAndLastUsed(t *testing.T) {
	s, _ := openStore(t)
	a := mustAdd(t, s, "a", "青")
	b := mustAdd(t, s, "b", "青")
	mustAdd(t, s, "c", "赤")
	if err := s.AddTopic("空"); err != nil {
		t.Fatal(err)
	}
	got := s.TopicStats()
	if len(got) != 3 || got[0].Name != "青" || got[0].Count != 2 || got[1].Count != 1 || got[2].Count != 0 || got[2].LastUsed != nil {
		t.Fatalf("TopicStats = %+v", got)
	}
	if !got[0].LastUsed.Equal(b.CreatedAt) || !got[0].LastUsed.After(a.CreatedAt) {
		t.Errorf("LastUsed should be the newest memo's time: %v", got[0].LastUsed)
	}
}

func TestRankByRecent(t *testing.T) {
	at := func(m int) *time.Time { v := time.Date(2026, 10, 1, 0, m, 0, 0, time.UTC); return &v }
	in := []TopicStat{{Name: "none1"}, {Name: "old", LastUsed: at(1)}, {Name: "none2"}, {Name: "new", LastUsed: at(9)}}
	var names []string
	for _, t := range RankByRecent(in) {
		names = append(names, t.Name)
	}
	if want := []string{"new", "old", "none1", "none2"}; !reflect.DeepEqual(names, want) {
		t.Errorf("RankByRecent = %v, want %v", names, want)
	}
	if in[0].Name != "none1" {
		t.Error("input must not be reordered")
	}
}

func TestRenameTopic(t *testing.T) {
	s, dir := openStore(t)
	n := mustAdd(t, s, "a", "青")
	other := mustAdd(t, s, "b", "赤")
	if err := s.RenameTopic("青", " 青分離 "); err != nil {
		t.Fatal(err)
	}
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"青分離", "赤"}) {
		t.Errorf("Topics = %v (renamed in place)", got)
	}
	if got, _ := s.Get(n.ID); got.Topic != "青分離" {
		t.Errorf("memo topic = %q", got.Topic)
	}
	if got, _ := s.Get(other.ID); got.Topic != "赤" {
		t.Errorf("other memo changed: %q", got.Topic)
	}
	assertEventOps(t, dir, "rename_topic")
}

func TestRenameTopicOntoExistingMerges(t *testing.T) {
	s, _ := openStore(t)
	a := mustAdd(t, s, "a", "あお")
	b := mustAdd(t, s, "b", "青")
	if err := s.RenameTopic("あお", "青"); err != nil {
		t.Fatal(err)
	}
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"青"}) {
		t.Errorf("Topics = %v", got)
	}
	for _, id := range []int{a.ID, b.ID} {
		if got, _ := s.Get(id); got.Topic != "青" {
			t.Errorf("memo %d topic = %q", id, got.Topic)
		}
	}
}

func TestRenameTopicRejects(t *testing.T) {
	s, dir := openStore(t)
	mustAdd(t, s, "a", "青")
	var inv InvalidError
	for _, to := range []string{"", "  ", "青"} {
		if err := s.RenameTopic("青", to); !errors.As(err, &inv) {
			t.Errorf("RenameTopic(青, %q) err = %v, want InvalidError", to, err)
		}
	}
	if err := s.RenameTopic("無い", "x"); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("unknown topic err = %v", err)
	}
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"青"}) {
		t.Errorf("rejected calls changed Topics: %v", got)
	}
	assertEventOps(t, dir) // no topic operation was logged
}

func TestMergeTopic(t *testing.T) {
	s, dir := openStore(t)
	a := mustAdd(t, s, "a", "旧")
	b := mustAdd(t, s, "b", "新")
	if err := s.MergeTopic("旧", "新"); err != nil {
		t.Fatal(err)
	}
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"新"}) {
		t.Errorf("Topics = %v", got)
	}
	for _, id := range []int{a.ID, b.ID} {
		if got, _ := s.Get(id); got.Topic != "新" {
			t.Errorf("memo %d topic = %q", id, got.Topic)
		}
	}
	assertEventOps(t, dir, "merge_topic")

	var inv InvalidError
	if err := s.MergeTopic("新", "新"); !errors.As(err, &inv) {
		t.Errorf("merge into itself err = %v", err)
	}
	if err := s.MergeTopic("新", "どこにもない"); !errors.As(err, &inv) {
		t.Errorf("merge into a missing topic err = %v, want InvalidError", err)
	}
	if err := s.MergeTopic("どこにもない", "新"); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("merge of a missing topic err = %v", err)
	}
}

func TestDeleteTopic(t *testing.T) {
	s, dir := openStore(t)
	n := mustAdd(t, s, "a", "青")
	mustAdd(t, s, "b", "赤")
	if err := s.DeleteTopic("青"); err != nil {
		t.Fatal(err)
	}
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"赤"}) {
		t.Errorf("Topics = %v", got)
	}
	if got, err := s.Get(n.ID); err != nil || got.Topic != "" {
		t.Errorf("memo should stay, without a topic: %+v, %v", got, err)
	}
	assertEventOps(t, dir, "delete_topic")
	if err := s.DeleteTopic("青"); !errors.Is(err, ErrTopicNotFound) {
		t.Errorf("second delete err = %v", err)
	}
}

func TestTopicChangesSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, s, "a", "青")
	mustAdd(t, s, "b", "赤")
	if err = s.RenameTopic("青", "青分離"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTopic("赤"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got := topicNames(s); !reflect.DeepEqual(got, []string{"青分離"}) {
		t.Errorf("Topics after reopen = %v", got)
	}
}

// assertEventOps checks that the topic operations in the log, in order, are
// exactly want (add_note / add_topic / set_topic lines are not counted).
func assertEventOps(t *testing.T, dir string, want ...string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	var got []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var ev Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(ev.Op, "_topic") && ev.Op != "add_topic" && ev.Op != "set_topic" {
				got = append(got, ev.Op)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("topic events = %v, want %v", got, want)
	}
}
