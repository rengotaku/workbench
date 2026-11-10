//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/rengotaku/workbench/internal/task"
)

// handoff seeds one task: waiting on `to` since daysAgo days (to == "" leaves it
// in the queue), with an optional due date.
type handoff struct {
	name    string
	to      string
	due     string
	daysAgo int
}

// startAppWithHandoffs runs the app on tasks handed on in the past. The server
// cannot back-date a handoff, so the tasks are written first and tasks.json is
// then edited, as the store's own tests do. It returns the ids by name.
func startAppWithHandoffs(t *testing.T, seeds ...handoff) (*app, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	ids := seedHandoffs(t, dir, seeds)
	backdateHandoffs(t, filepath.Join(dir, "tasks.json"), ids, seeds)
	return startAppIn(t, t.TempDir(), dir), ids
}

func seedHandoffs(t *testing.T, dir string, seeds []handoff) map[string]string {
	t.Helper()
	s, err := task.Open(dir, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, h := range seeds {
		added, addErr := s.Add(h.name, "", h.due, task.ActorHuman)
		if addErr != nil {
			t.Fatal(addErr)
		}
		ids[h.name] = added.ID
		if h.to == "" {
			continue
		}
		if _, waitErr := s.Wait(added.ID, h.to, "", task.ActorHuman); waitErr != nil {
			t.Fatal(waitErr)
		}
	}
	if closeErr := s.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return ids
}

// backdateHandoffs sets handed_on in tasks.json (the server is not running).
func backdateHandoffs(t *testing.T, path string, ids map[string]string, seeds []handoff) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err = json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	tasks, ok := st["tasks"].([]any)
	if !ok {
		t.Fatalf("tasks.json has no tasks: %s", raw)
	}
	for _, x := range tasks {
		k, isMap := x.(map[string]any)
		if !isMap {
			t.Fatalf("task is not an object: %v", x)
		}
		for _, h := range seeds {
			if ids[h.name] == k["id"] && h.to != "" {
				k["handed_on"] = day(-h.daysAgo)
			}
		}
	}
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// waitIDs is a JS expression: the ids of the rows in the waiting list, with
// "stub" for the line left behind, in the order drawn.
const waitIDs = `Array.from(document.querySelectorAll('#waitlist > li')).map(li => li.classList.contains('resumed-stub') ? 'stub' : li.dataset.id)`

// resumeFromWaiting opens the waiting section and the row, and presses 着手順へ戻す.
func (p *page) resumeFromWaiting(id string) {
	p.t.Helper()
	if p.str(`String(document.getElementById('waitlist').hidden)`) == "true" {
		p.click(`#wait-fold`)
	}
	p.openRow(id)
	p.clickButton(id, "着手順へ戻す")
}

// 着手順へ戻す leaves a line where the task stood and tints the row it became;
// the line's 元に戻す puts the task back with whoever held it and the day it was
// handed on, read from the server.
func TestTodoResumeLeavesALineAndItUndoes(t *testing.T) {
	a, id := startAppWithHandoffs(t,
		handoff{name: "q1"},
		handoff{name: "w1", to: "鈴木", daysAgo: 8},
		handoff{name: "w2", to: "高橋", daysAgo: 3},
		handoff{name: "w3", to: "佐藤", daysAgo: 1},
	)
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)
	p.resumeFromWaiting(id["w2"])

	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)
	if got := p.str(waitIDs + `.join()`); got != id["w1"]+",stub,"+id["w3"] {
		t.Errorf("waiting list = %s, want the line where w2 stood (between w1 and w3)", got)
	}
	text := p.str(`document.querySelector('#waitlist .resumed-stub .rs-text').textContent`)
	want := id["w2"] + " を着手順の末尾へ移した（高橋 へ " + monthDayOf(day(-3)) + " に渡していた分）"
	if text != want {
		t.Errorf("line = %q, want %q", text, want)
	}
	p.until("row tinted and tagged", `document.querySelectorAll('#queue .task.landed .resumed-tag').length === 1 && document.querySelector('#queue .task.landed').dataset.id === '`+id["w2"]+`'`)
	if got := p.str(`document.querySelector('#queue .task.landed .resumed-tag').textContent`); got != "待ちから戻した" {
		t.Errorf("tag = %q", got)
	}
	if n := p.num(`document.querySelectorAll('.task.landed').length`); n != 1 {
		t.Errorf("tinted rows = %v, want 1", n)
	}
	// The notice up the page is not used: it is far from the click.
	if got := p.str(noticeText); got != "" {
		t.Errorf("notice = %q, want none", got)
	}

	p.eval(`document.querySelector('#waitlist .resumed-stub .rs-undo').click()`, nil)
	p.until("back in waiting", `document.querySelectorAll('#waitlist .resumed-stub').length === 0 && `+waitIDs+`.join() === '`+id["w1"]+`,`+id["w2"]+`,`+id["w3"]+`'`)
	got := a.task(t, id["w2"])
	if str(got, "state") != "waiting" || str(got, "handed_to") != "高橋" || str(got, "handed_on") != day(-3) {
		t.Errorf("after undo: state=%s handed_to=%s handed_on=%s, want waiting 高橋 %s",
			str(got, "state"), str(got, "handed_to"), str(got, "handed_on"), day(-3))
	}
	if n := p.num(`document.querySelectorAll('.task.landed, .resumed-tag').length`); n != 0 {
		t.Errorf("tint and tag left after undo (%v)", n)
	}
}

func monthDayOf(date string) string {
	d, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	return strings.TrimLeft(d.Format("01"), "0") + "/" + strings.TrimLeft(d.Format("02"), "0")
}

// The line and the tag go after 10 seconds, and not before: the poll's redraw
// in between leaves them alone.
func TestTodoResumeLineGoesAfterTenSeconds(t *testing.T) {
	a, id := startAppWithHandoffs(t, handoff{name: "w1", to: "高橋", daysAgo: 3})
	p := newPage(t, a, "/")
	p.until("waiting", `document.getElementById('wait-count').textContent === '1'`)
	p.resumeFromWaiting(id["w1"])
	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)
	start := time.Now()

	// The poll runs every 4 seconds: by now at least one tick has found the
	// revision as it was and drawn nothing new.
	time.Sleep(6 * time.Second)
	if n := p.num(`document.querySelectorAll('#waitlist .resumed-stub').length + document.querySelectorAll('.resumed-tag').length`); n != 2 {
		t.Fatalf("after 6s the line and the tag = %v of 2, want both (the poll must not clear them)", n)
	}
	p.untilWithin(8*time.Second, "line and tag gone", `document.querySelectorAll('#waitlist .resumed-stub, .resumed-tag, .task.landed').length === 0`)
	if d := time.Since(start); d < 9*time.Second {
		t.Errorf("gone after %v, want about 10s", d)
	}
}

// The next operation clears the line: here, completing another task.
func TestTodoResumeLineGoesAtTheNextOperation(t *testing.T) {
	a, id := startAppWithHandoffs(t,
		handoff{name: "q1"},
		handoff{name: "w1", to: "高橋", daysAgo: 3},
	)
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)
	p.resumeFromWaiting(id["w1"])
	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)

	p.click(`#queue .task[data-id="` + id["q1"] + `"] .check`)
	p.until("line and tag gone", `document.querySelectorAll('#waitlist .resumed-stub, .resumed-tag, .task.landed').length === 0`)
}

// A change made elsewhere after the resume (the terminal) clears the line, and
// 元に戻す pressed in the gap does not undo that change: the server refuses.
func TestTodoResumeUndoRefusesAfterAnotherChange(t *testing.T) {
	a, id := startAppWithHandoffs(t,
		handoff{name: "q1"},
		handoff{name: "w1", to: "高橋", daysAgo: 3},
	)
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)
	p.resumeFromWaiting(id["w1"])
	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)

	// Whether the poll gets to this first or the click does, the rename stays.
	a.patch(t, id["q1"], map[string]any{"name": "q1 renamed"})
	p.eval(`document.querySelector('#waitlist .resumed-stub .rs-undo') && document.querySelector('#waitlist .resumed-stub .rs-undo').click()`, nil)
	p.until("line gone", `document.querySelectorAll('#waitlist .resumed-stub').length === 0`)

	if got := a.task(t, id["q1"]); str(got, "name") != "q1 renamed" {
		t.Errorf("q1 = %q, the later rename was undone", str(got, "name"))
	}
	if got := a.task(t, id["w1"]); str(got, "state") != "open" {
		t.Errorf("w1 state = %q, want it still resumed", str(got, "state"))
	}
}

// With reduced motion the row and the line are only coloured; otherwise they
// blink (a CSS animation of three half-second turns).
func TestTodoResumeBlinkFollowsReducedMotion(t *testing.T) {
	a, id := startAppWithHandoffs(t,
		handoff{name: "w1", to: "高橋", daysAgo: 3},
		handoff{name: "w2", to: "鈴木", daysAgo: 2},
	)
	p := newPage(t, a, "/")
	p.until("waiting", `document.getElementById('wait-count').textContent === '2'`)
	anim := func(sel string) string {
		return p.str(`(e => e ? getComputedStyle(e).animationName + ' ' + getComputedStyle(e).animationDuration + ' ' + getComputedStyle(e).animationIterationCount : 'missing')(document.querySelector('` + sel + `'))`)
	}

	p.resumeFromWaiting(id["w1"])
	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)
	for _, sel := range []string{`#queue .task.landed`, `#waitlist .resumed-stub`} {
		if got := anim(sel); got != "resumed-blink 0.5s 3" {
			t.Errorf("%s animation = %q, want resumed-blink 0.5s 3", sel, got)
		}
	}

	p.run(chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}).Do(ctx)
	}))
	p.resumeFromWaiting(id["w2"])
	p.until("second line left", `document.querySelector('#waitlist .resumed-stub').dataset.stub === '`+id["w2"]+`'`)
	for _, sel := range []string{`#queue .task.landed`, `#waitlist .resumed-stub`} {
		if got := anim(sel); !strings.HasPrefix(got, "none ") {
			t.Errorf("%s animation = %q under reduced motion, want none", sel, got)
		}
	}
	if bg := p.str(`getComputedStyle(document.querySelector('#queue .task.landed')).backgroundColor`); bg == "rgba(0, 0, 0, 0)" || bg == "transparent" {
		t.Errorf("landed row has no colour under reduced motion (%s)", bg)
	}
}

// Where the task went is said in the line even when it went somewhere closed
// (スコープ外), and 見る opens that section and brings the row into view.
func TestTodoResumeLineLeadsToARowOutOfScope(t *testing.T) {
	a, id := startAppWithHandoffs(t,
		handoff{name: "far", to: "高橋", due: day(40), daysAgo: 3},
	)
	p := newPage(t, a, "/")
	p.until("waiting", `document.getElementById('wait-count').textContent === '1'`)
	p.resumeFromWaiting(id["far"])
	p.until("line left", `document.querySelectorAll('#waitlist .resumed-stub').length === 1`)

	if got := p.str(`document.querySelector('#waitlist .resumed-stub .rs-text').textContent`); !strings.Contains(got, "スコープ外（期限 ") {
		t.Errorf("line = %q, want it to say the task went out of scope", got)
	}
	if got := p.str(`String(document.getElementById('outlist').hidden)`); got != "true" {
		t.Fatalf("the スコープ外 section is open before 見る (hidden=%s)", got)
	}
	p.eval(`Array.from(document.querySelectorAll('#waitlist .resumed-stub button')).find(b => b.textContent === '見る').click()`, nil)
	p.until("section open and row in view", `!document.getElementById('outlist').hidden && (r => r && r.top >= 0 && r.bottom <= window.innerHeight)(document.querySelector('#outlist .task[data-id="`+id["far"]+`"]')?.getBoundingClientRect())`)
	if n := p.num(`document.querySelectorAll('#outlist .task.landed .resumed-tag').length`); n != 1 {
		t.Errorf("the row 見る went to is not tinted and tagged (%v)", n)
	}
	if n := p.num(`document.querySelectorAll('#waitlist .resumed-stub').length`); n != 1 {
		t.Errorf("見る removed the line (%v)", n)
	}
}
