//go:build e2e

package e2e

import (
	"fmt"
	"math"
	"testing"

	"github.com/chromedp/chromedp"
)

// Moving between screens through the header does not load the page again, and
// what a screen holds is still there when you come back: the open row, the
// scroll position, a half-typed memo.
func TestScreensKeepStateAcrossNavigation(t *testing.T) {
	a := startApp(t)
	for i := 1; i <= 40; i++ {
		a.addTask(t, fmt.Sprintf("task-%02d", i))
	}
	p := newPage(t, a, "/")
	p.until("40 rows", `document.querySelectorAll('#queue .task').length === 40`)
	p.eval(`window.__marker = 'same-page'`, nil)

	// Open a row far down, then scroll somewhere else.
	p.eval(`document.querySelectorAll('#queue .task')[20].click()`, nil)
	p.until("row open", `document.querySelectorAll('.task.row-open').length === 1`)
	openName := p.str(`document.querySelector('.task.row-open .txt').textContent.trim()`)
	p.eval(`window.scrollTo(0, 500)`, nil)
	p.until("scrolled", `window.scrollY > 100`)
	scrolled := p.num(`window.scrollY`)

	// note add: type a draft, leave, come back.
	p.nav("/notes")
	p.until("note add shown", shown("note-add"))
	p.run(chromedp.SendKeys(`#memo`, "書きかけのメモ", chromedp.ByQuery))
	p.nav("/notes/timeline")
	p.until("timeline shown", shown("timeline"))
	p.nav("/notes")
	p.until("note add shown again", shown("note-add"))
	if got := p.str(`document.getElementById('memo').value`); got != "書きかけのメモ" {
		t.Errorf("draft after coming back = %q, want it kept", got)
	}

	// todo list: same open row, same scroll.
	p.nav("/")
	p.until("todo shown", shown("todo"))
	if got := p.str(`window.__marker`); got != "same-page" {
		t.Errorf("window marker = %q: the page was loaded again", got)
	}
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Fatalf("open rows = %v, want 1", n)
	}
	if got := p.str(`document.querySelector('.task.row-open .txt').textContent.trim()`); got != openName {
		t.Errorf("open row = %q, want %q", got, openName)
	}
	if got := p.num(`window.scrollY`); math.Abs(got-scrolled) > 2 {
		t.Errorf("scrollY = %v, want %v", got, scrolled)
	}
	if got := p.path(); got != "/" {
		t.Errorf("path = %q, want /", got)
	}
}

// A screen that is hidden does not take N or Esc.
func TestHiddenScreensIgnoreKeys(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "row-a")
	p := newPage(t, a, "/")
	p.until("row", `document.querySelectorAll('#queue .task').length === 1`)

	// Open the row, then visit the other screens so each one is mounted.
	p.eval(`document.querySelector('#queue .task').click()`, nil)
	p.until("row open", `document.querySelectorAll('.task.row-open').length === 1`)
	p.nav("/notes")
	p.until("note add shown", shown("note-add"))
	p.nav("/notes/timeline")
	p.until("timeline shown", shown("timeline"))

	// On the timeline, Esc is nobody's: the todo list must not fold its row.
	p.esc()
	// N opens the memo dialog and only that one.
	p.key("n")
	p.until("a dialog", `document.querySelectorAll('dialog[open]').length === 1`)
	if got := p.str(`document.querySelector('dialog[open]').getAttribute('aria-label')`); got != "メモを追加" {
		t.Errorf("dialog on the timeline = %q, want メモを追加", got)
	}
	p.esc()
	p.until("dialog closed", `document.querySelectorAll('dialog[open]').length === 0`)
	if got := p.path(); got != "/notes/timeline" {
		t.Errorf("path after Esc = %q, want to stay on the timeline", got)
	}

	p.nav("/")
	p.until("todo shown", shown("todo"))
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Fatalf("row folded by a hidden screen's Esc: open rows = %v, want 1", n)
	}

	// On the list, N opens the task dialog; while it is open Esc closes only
	// the dialog, not the row behind it.
	p.key("n")
	p.until("a dialog", `document.querySelectorAll('dialog[open]').length === 1`)
	if got := p.str(`document.querySelector('dialog[open]').getAttribute('aria-label')`); got != "タスクを追加" {
		t.Errorf("dialog on the list = %q, want タスクを追加", got)
	}
	p.esc()
	p.until("dialog closed", `document.querySelectorAll('dialog[open]').length === 0`)
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Errorf("Esc in the dialog folded the row: open rows = %v, want 1", n)
	}

	// Now Esc folds the row, and the memo screen's Esc (leave for the
	// timeline) does not run from behind.
	p.esc()
	p.until("row folded", `document.querySelectorAll('.task.row-open').length === 0`)
	if got := p.path(); got != "/" {
		t.Errorf("path after Esc on the list = %q, want /", got)
	}
}

// N opens the add dialog, Esc closes it, Back closes it, and what was added
// there shows in the list.
func TestAddDialog(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "existing")
	p := newPage(t, a, "/")
	p.until("row", `document.querySelectorAll('#queue .task').length === 1`)

	p.key("n")
	p.until("dialog open", `document.querySelector('dialog[open] #ap-name')`)
	p.esc()
	p.until("Esc closes it", `document.querySelectorAll('dialog[open]').length === 0`)
	p.until("add screen returned", `document.querySelector('[data-view="todo-add"]').hidden && !document.querySelector('dialog')`)

	// Back closes it: open it over the timeline, then go back to the list.
	p.nav("/notes/timeline")
	p.until("timeline shown", shown("timeline"))
	p.key("n")
	p.until("dialog open", `document.querySelector('dialog[open] #memo')`)
	p.run(chromedp.NavigateBack())
	p.until("back on the list", shown("todo")+` && location.pathname === '/'`)
	p.until("Back closed the dialog", `document.querySelectorAll('dialog[open]').length === 0`)
	p.until("memo screen returned", `document.querySelector('[data-view="note-add"]').hidden`)

	// Add a task through the dialog.
	p.key("n")
	p.until("dialog open", `document.querySelector('dialog[open] #ap-name')`)
	// The CDP key event types the "n" that opened the dialog into the field
	// (a real keydown that was preventDefault-ed types nothing): start empty.
	p.eval(`document.getElementById("ap-name").value = ""`, nil)
	p.run(chromedp.SendKeys(`#ap-name`, "ダイアログで追加", chromedp.ByQuery))
	p.key("\r")
	p.until("listed in the dialog", `Array.from(document.querySelectorAll('dialog[open] .ap-recent-name')).some(e => e.textContent === 'ダイアログで追加')`)
	p.esc()
	p.until("dialog closed", `document.querySelectorAll('dialog[open]').length === 0`)
	p.until("new row in the list", `Array.from(document.querySelectorAll('#queue .txt')).some(e => e.textContent.trim() === 'ダイアログで追加')`)
}
