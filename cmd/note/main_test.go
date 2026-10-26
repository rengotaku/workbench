package main

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteapi"
	"github.com/rengotaku/workbench/internal/noteroute"
	"github.com/rengotaku/workbench/internal/task"
)

type result struct {
	out    string
	errOut string
	code   int
}

// harness runs the CLI against the real API over a temp store.
type harness struct {
	t      *testing.T
	openFn func(string) error
	url    string
	opened []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := task.Open(t.TempDir(), time.Local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tasks.Close(); _ = store.Close() })
	r := gin.New()
	noteapi.New(store).Mount(r)
	noteroute.New(store, tasks, time.Local).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Setenv("NOTE_SERVER", "")
	return &harness{t: t, url: srv.URL}
}

// run executes the CLI with stdin text (tty=false) or a terminal (tty=true).
func (h *harness) run(stdin string, tty bool, args ...string) result {
	h.t.Helper()
	var out, errOut bytes.Buffer
	e := &env{
		stdin:      strings.NewReader(stdin),
		stdinIsTTY: tty,
		stdout:     &out,
		stderr:     &errOut,
		openURL: func(u string) error {
			h.opened = append(h.opened, u)
			if h.openFn != nil {
				return h.openFn(u)
			}
			return nil
		},
	}
	code := execute(e, append([]string{"--server", h.url}, args...))
	return result{out: out.String(), errOut: errOut.String(), code: code}
}

func (h *harness) ok(args ...string) string {
	h.t.Helper()
	r := h.run("", true, args...)
	if r.code != 0 {
		h.t.Fatalf("note %v: exit %d, stderr %q", args, r.code, r.errOut)
	}
	return r.out
}

func TestAddFromArgs(t *testing.T) {
	h := newHarness(t)
	if got := h.ok("https://x.slack.com/a/1", "staging", "変わった"); got != "note #1 保存\n" {
		t.Errorf("out = %q", got)
	}
	if got := h.ok("--topic", "青分離", "二つ目"); got != "note #2 保存\n" {
		t.Errorf("out = %q", got)
	}
	list := h.ok("list", "--topic", "青分離")
	if !strings.Contains(list, "[青分離]  二つ目  (補足 0)") || strings.Contains(list, "staging") {
		t.Errorf("list = %q", list)
	}
	if len(h.opened) != 0 {
		t.Error("adding with arguments must not open the browser")
	}
}

func TestAddFromStdin(t *testing.T) {
	h := newHarness(t)
	r := h.run("  https://x.notion.so/p 議事録\n", false)
	if r.code != 0 || r.out != "note #1 保存\n" {
		t.Fatalf("result = %+v", r)
	}
	got := h.ok("list", "--json")
	if !strings.Contains(got, `"text":"https://x.notion.so/p 議事録"`) {
		t.Errorf("stdin text should be trimmed and stored whole: %s", got)
	}
}

func TestAddFromStdinEmptyFails(t *testing.T) {
	h := newHarness(t)
	r := h.run("  \n ", false)
	if r.code != 1 || !strings.Contains(r.errOut, "メモが空です") {
		t.Errorf("result = %+v", r)
	}
	if r := h.run("", false, "   "); r.code != 1 {
		t.Errorf("blank argument should fail too: %+v", r)
	}
}

func TestAddJSON(t *testing.T) {
	h := newHarness(t)
	got := h.ok("--json", "メモ x")
	if !strings.HasPrefix(got, "{") || !strings.Contains(got, `"id":1`) {
		t.Errorf("--json out = %q", got)
	}
}

func TestBareNoteOpensBrowserOnTerminal(t *testing.T) {
	h := newHarness(t)
	r := h.run("", true)
	if r.code != 0 || len(h.opened) != 1 || h.opened[0] != h.url+"/notes" {
		t.Fatalf("result = %+v, opened = %v", r, h.opened)
	}
	if !strings.Contains(r.out, h.url) {
		t.Errorf("out = %q", r.out)
	}
}

func TestBareNoteOpenFailure(t *testing.T) {
	h := newHarness(t)
	h.openFn = func(string) error { return errors.New("no display") }
	r := h.run("", true)
	if r.code != 1 || !strings.Contains(r.errOut, "ブラウザを開けませんでした") {
		t.Errorf("result = %+v", r)
	}
}

func TestBareNoteWithBadServerURL(t *testing.T) {
	var out, errOut bytes.Buffer
	e := &env{stdin: strings.NewReader(""), stdinIsTTY: true, stdout: &out, stderr: &errOut, openURL: func(string) error { return nil }}
	if code := execute(e, []string{"--server", "nonsense"}); code != 1 || !strings.Contains(errOut.String(), "URL が正しくありません") {
		t.Errorf("code %d, err %q", code, errOut.String())
	}
}

func TestListAndShow(t *testing.T) {
	h := newHarness(t)
	h.ok("https://x.slack.com/a/1", "デプロイ")
	h.ok("--topic", "C&C", "別のメモ")
	body := filepath.Join(t.TempDir(), "q.md")
	if err := os.WriteFile(body, []byte("剣持: 共有します\n・deploy_staging\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.ok("annotate", "1", "--kind", "quote", "--source", "slack", "--body-file", body)
	h.ok("annotate", "#1", "--kind", "analysis", "--body", "考察です", "--author", "human")
	h.ok("route", "1", "--record")

	if got := h.ok("list", "--pending"); strings.Contains(got, "デプロイ") || !strings.Contains(got, "#2  ") {
		t.Errorf("pending list = %q", got)
	}
	if got := h.ok("list", "--no-topic"); !strings.Contains(got, "#1  ") || strings.Contains(got, "#2  ") {
		t.Errorf("no-topic list = %q", got)
	}
	if got := h.ok("list"); strings.Index(got, "#2  ") > strings.Index(got, "#1  ") || !strings.Contains(got, "(補足 2)") {
		t.Errorf("list = %q", got)
	}

	show := h.ok("show", "1")
	for _, want := range []string{
		"#1  ", "デプロイ", "リンク: https://x.slack.com/a/1（Slack）",
		"--- quote · ai · ", " · slack\n剣持: 共有します\n・deploy_staging",
		"--- analysis · human · ", "\n考察です",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("show output should contain %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "補足待ち") || !strings.Contains(show, "→ 記録") {
		t.Errorf("a routed memo is not pending and shows where it went:\n%s", show)
	}
	if got := h.ok("show", "2"); !strings.Contains(got, "補足待ち") {
		t.Errorf("show of a bare memo = %q", got)
	}
	if got := h.ok("show", "1", "--json"); !strings.Contains(got, `"annotations"`) {
		t.Errorf("show --json = %q", got)
	}
	if got := h.ok("list", "--json"); !strings.Contains(got, `"pending"`) {
		t.Errorf("list --json = %q", got)
	}
}

func TestAnnotateErrors(t *testing.T) {
	h := newHarness(t)
	h.ok("メモ a")
	missing := filepath.Join(t.TempDir(), "none.md")
	for _, tc := range []struct {
		name string
		want string
		args []string
	}{
		{"no kind", "--kind", []string{"annotate", "1", "--body", "x"}},
		{"no body", "--body か --body-file", []string{"annotate", "1", "--kind", "memo"}},
		{"both bodies", "--body か --body-file", []string{"annotate", "1", "--kind", "memo", "--body", "x", "--body-file", missing}},
		{"missing file", "--body-file を読めません", []string{"annotate", "1", "--kind", "memo", "--body-file", missing}},
		{"unknown kind", "quote / analysis / memo", []string{"annotate", "1", "--kind", "essay", "--body", "x"}},
		{"empty body", "body が空です", []string{"annotate", "1", "--kind", "memo", "--body", " "}},
		{"unknown id", "見つかりません", []string{"annotate", "9", "--kind", "memo", "--body", "x"}},
		{"bad id", "id は 1 以上", []string{"annotate", "abc", "--kind", "memo", "--body", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h.run("", true, tc.args...)
			if r.code != 1 || !strings.Contains(r.errOut, tc.want) {
				t.Errorf("result = %+v, want stderr containing %q", r, tc.want)
			}
		})
	}
	if got := h.ok("show", "1"); strings.Contains(got, "---") {
		t.Error("failed annotations must not be stored")
	}
}

func TestTopicCommands(t *testing.T) {
	h := newHarness(t)
	h.ok("メモ a")
	if got := h.ok("topic", "1", "青分離"); got != "note #1 話題 青分離\n" {
		t.Errorf("out = %q", got)
	}
	if got := h.ok("topics"); got != "青分離\n" {
		t.Errorf("topics = %q", got)
	}
	if got := h.ok("topic", "1", "--clear"); got != "note #1 話題を外した\n" {
		t.Errorf("out = %q", got)
	}
	for _, args := range [][]string{
		{"topic", "1"},
		{"topic", "1", "x", "--clear"},
		{"topic", "9", "x"},
		{"topic", "0", "x"},
	} {
		if r := h.run("", true, args...); r.code != 1 || r.errOut == "" {
			t.Errorf("%v: result = %+v", args, r)
		}
	}
}

func TestDelete(t *testing.T) {
	h := newHarness(t)
	h.ok("メモ a")
	if got := h.ok("delete", "1"); got != "note #1 削除\n" {
		t.Errorf("out = %q", got)
	}
	if r := h.run("", true, "delete", "1"); r.code != 1 || !strings.Contains(r.errOut, "見つかりません") {
		t.Errorf("second delete = %+v", r)
	}
	if r := h.run("", true, "delete", "x"); r.code != 1 {
		t.Errorf("bad id = %+v", r)
	}
}

func TestServerDownExitsOne(t *testing.T) {
	srv := httptest.NewServer(nil)
	down := srv.URL
	srv.Close()
	t.Setenv("NOTE_SERVER", down) // also covers the env fallback

	for _, tc := range []struct {
		stdin string
		args  []string
		tty   bool
	}{
		{"", []string{"メモ"}, true},
		{"stdin メモ", nil, false},
		{"", []string{"list"}, true},
		{"", []string{"show", "1"}, true},
		{"", []string{"annotate", "1", "--kind", "memo", "--body", "x"}, true},
		{"", []string{"topic", "1", "x"}, true},
		{"", []string{"topics"}, true},
		{"", []string{"delete", "1"}, true},
		{"", nil, true}, // bare `note` still resolves the URL, but the browser open succeeds
	} {
		var out, errOut bytes.Buffer
		e := &env{stdin: strings.NewReader(tc.stdin), stdinIsTTY: tc.tty, stdout: &out, stderr: &errOut, openURL: func(string) error { return nil }}
		code := execute(e, tc.args)
		if tc.args == nil && tc.tty {
			if code != 0 {
				t.Errorf("bare note should only need the URL, got exit %d: %s", code, errOut.String())
			}
			continue
		}
		if code != 1 || !strings.Contains(errOut.String(), "サーバに接続できません") {
			t.Errorf("%v: exit %d, stderr %q", tc.args, code, errOut.String())
		}
		if out.Len() != 0 {
			t.Errorf("%v: nothing should be printed to stdout on failure, got %q", tc.args, out.String())
		}
	}
}

func TestHelp(t *testing.T) {
	h := newHarness(t)
	r := h.run("", true, "help")
	if r.code != 0 || !strings.Contains(r.out, "annotate") || !strings.Contains(r.out, "pbpaste | note") {
		t.Errorf("help = %+v", r)
	}
}

func TestParseID(t *testing.T) {
	for in, want := range map[string]int{"1": 1, "#41": 41} {
		if got, err := parseID(in); err != nil || got != want {
			t.Errorf("parseID(%q) = %d, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "0", "-1", "a", "#"} {
		if _, err := parseID(bad); err == nil {
			t.Errorf("parseID(%q) should fail", bad)
		}
	}
}

func TestAddWithLinks(t *testing.T) {
	h := newHarness(t)
	if got := h.ok("[1] で決定", "--link", "https://x.slack.com/a/1 決定スレッド", "--link", "https://space.backlog.com/view/DSC-1"); got != "note #1 保存\n" {
		t.Errorf("out = %q", got)
	}
	show := h.ok("show", "1")
	for _, want := range []string{
		"\n  [1] https://x.slack.com/a/1（Slack） 決定スレッド",
		"\n  [2] https://space.backlog.com/view/DSC-1（Backlog）",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("show should contain %q:\n%s", want, show)
		}
	}

	r := h.run("", true, "[3] は無い", "--link", "https://x.example/")
	if r.code != 1 || !strings.Contains(r.errOut, "[3] に当たる関連 URL がありません") {
		t.Errorf("result = %+v", r)
	}
}

func TestLinkWithoutTextFails(t *testing.T) {
	h := newHarness(t)
	r := h.run("", true, "--link", "https://x.example/")
	if r.code != 1 || !strings.Contains(r.errOut, "本文も渡してください") || len(h.opened) != 0 {
		t.Errorf("result = %+v, opened %v", r, h.opened)
	}
}

func TestRouteCommand(t *testing.T) {
	h := newHarness(t)
	h.ok("確認する")
	h.ok("あとで読む")

	out := h.ok("route", "1", "--todo", "確認する", "--due", "2026-12-01", "--detail", "詳細")
	if !strings.Contains(out, "note #1 → todo T001") {
		t.Errorf("route --todo output = %q", out)
	}
	if again := h.ok("route", "#1", "--todo", "確認する"); !strings.Contains(again, "todo T001") {
		t.Errorf("rerun should report the same task: %q", again)
	}
	if got := h.ok("route", "2", "--record"); !strings.Contains(got, "note #2 → 記録") {
		t.Errorf("route --record output = %q", got)
	}
	if got := h.ok("list", "--pending"); strings.TrimSpace(got) != "" {
		t.Errorf("nothing should be pending: %q", got)
	}
	if got := h.ok("show", "1"); !strings.Contains(got, "→ todo T001") {
		t.Errorf("show = %q", got)
	}
	if got := h.ok("route", "1", "--todo", "x", "--json"); !strings.Contains(got, `"source_note_id": 1`) && !strings.Contains(got, `"source_note_id":1`) {
		t.Errorf("--json = %q", got)
	}

	for _, tc := range []struct {
		name string
		want string
		args []string
	}{
		{"neither", "どちらか一方", []string{"route", "1"}},
		{"both", "どちらか一方", []string{"route", "1", "--todo", "x", "--record"}},
		{"due with record", "--todo のときだけ", []string{"route", "1", "--record", "--due", "2026-12-01"}},
		{"blank name", "名前を渡して", []string{"route", "1", "--todo", " "}},
		{"conflict", "振り分け済み", []string{"route", "2", "--todo", "x"}},
		{"unknown id", "見つかりません", []string{"route", "9", "--record"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h.run("", true, tc.args...)
			if r.code != 1 || !strings.Contains(r.errOut, tc.want) {
				t.Errorf("result = %+v, want stderr containing %q", r, tc.want)
			}
		})
	}
}

func TestAnnotateTooLongShowsTheServerError(t *testing.T) {
	h := newHarness(t)
	h.ok("メモ a")
	r := h.run("", true, "annotate", "1", "--kind", "memo", "--body", strings.Repeat("a", note.MaxAnnotationBody+1))
	if r.code != 1 || !strings.Contains(r.errOut, "長すぎます") {
		t.Errorf("result = %+v", r)
	}
}

// A mistyped subcommand used to become a memo (#97).
func TestUnknownSubcommandAddsNothing(t *testing.T) {
	h := newHarness(t)
	r := h.run("", true, "show-x", "1")
	if r.code != 1 || !strings.Contains(r.errOut, `"show-x" というサブコマンドはありません`) || !strings.Contains(r.errOut, `note "show-x 1"`) {
		t.Fatalf("result = %+v", r)
	}
	if got := h.ok("list", "--json"); strings.Contains(got, "show-x") {
		t.Errorf("nothing should be stored: %s", got)
	}
	if got := h.ok("--", "show-x", "1"); got != "note #1 保存\n" {
		t.Errorf("after -- it is a memo: %q", got)
	}
	if got := h.ok("show-x 1"); got != "note #2 保存\n" {
		t.Errorf("quoted as one argument it is a memo: %q", got)
	}
}
