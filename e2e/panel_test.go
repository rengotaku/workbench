//go:build e2e

package e2e

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Cmd/Ctrl+Z typed in the panel's name undoes the typing, not the list's
// history: the task done just before stays done.
func TestTodoDetailPanelUndoKeyStaysInTheField(t *testing.T) {
	a := startApp(t)
	done := a.addTask(t, "done before")
	open := a.addTask(t, "typing here")
	a.post(t, "/api/tasks/"+done+"/done", map[string]any{})
	p := newPage(t, a, "/")
	p.until("row", `!!document.querySelector('.task[data-id="`+open+`"]')`)
	p.eval(`document.querySelector('.task[data-id="`+open+`"] .txt').click(), true`, nil)
	p.until("panel open", `!document.getElementById('detail').hidden`)
	p.eval(`document.querySelector('#detail .d-name .txt.live').focus(), true`, nil)
	p.until("caret in the name", `document.activeElement && document.activeElement.isContentEditable`)
	p.eval(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown', {key: 'z', ctrlKey: true, metaKey: true, bubbles: true})), true`, nil)
	p.eval(`new Promise(r => setTimeout(() => r(true), 800))`, nil)
	if got := str(a.task(t, done), "state"); got != "done" {
		t.Errorf("state of the task done before = %q, want done (the list's undo ran)", got)
	}
}

const panelOpenFor =`(id => { const d = document.getElementById('detail'); return !d.hidden && !!d.querySelector('.d-id') && d.querySelector('.d-id').textContent === id; })`

// The detail panel opens only when a row is pressed, lies over the right of the
// list, and is replaced by another row; the same row, the cross and Escape close
// it and give the keyboard back to the row.
func TestTodoDetailPanelOpensAndCloses(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "first")
	two := a.addTask(t, "second")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	rowOf := func(id string) string { return `document.querySelector('.task[data-id="` + id + `"]')` }
	isOpen := func(id string) string { return panelOpenFor + `('` + id + `')` }

	// Nothing is shown at first, and the pointer alone changes nothing.
	if got := p.str(`String(document.getElementById('detail').hidden)`); got != "true" {
		t.Errorf("panel hidden before any press = %q, want true", got)
	}
	height := p.num(rowOf(one) + `.getBoundingClientRect().height`)
	p.eval(rowOf(one)+`.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true}))`, nil)
	p.eval(rowOf(one)+`.dispatchEvent(new MouseEvent('mouseover', {bubbles: true}))`, nil)
	if got := p.str(`String(document.getElementById('detail').hidden)`); got != "true" {
		t.Errorf("a pointer over a row opened the panel")
	}

	// A press opens it, over the right of the list, with the caret in the name.
	p.eval(rowOf(one)+`.click()`, nil)
	p.until("panel for the first", isOpen(one))
	p.until("the panel has the focus", `document.activeElement === document.getElementById('detail')`)
	if got := p.str(`getComputedStyle(document.getElementById('detail')).position`); got != "fixed" {
		t.Errorf("panel position = %q, want fixed", got)
	}
	if got := p.num(`document.getElementById('detail').getBoundingClientRect().right - window.innerWidth`); got != 0 {
		t.Errorf("panel right edge is %v px from the window's, want 0", got)
	}
	if got := p.num(`Math.abs(document.getElementById('detail').getBoundingClientRect().width - Math.max(360, window.innerWidth * 0.45))`); got > 2 {
		t.Errorf("panel width is off the 45%% rule by %v px", got)
	}
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Errorf("selected rows = %v, want 1", n)
	}
	if got := p.num(rowOf(one) + `.getBoundingClientRect().height`); got != height {
		t.Errorf("row height %v -> %v: the row grew", height, got)
	}
	if n := p.num(`document.querySelectorAll('.task .panel, .task .live').length`); n != 0 {
		t.Errorf("a panel opened under a row (%v)", n)
	}

	// Another row is pressed while it is open: the panel switches to it. The
	// pointer going over the first row again changes nothing.
	p.eval(rowOf(two)+`.click()`, nil)
	p.until("panel for the second", isOpen(two))
	p.eval(rowOf(one)+`.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true}))`, nil)
	if got := p.str(`document.querySelector('#detail .d-id').textContent`); got != two {
		t.Errorf("hover changed the panel to %q", got)
	}
	if got := p.str(`document.querySelector('.task.row-open').dataset.id`); got != two {
		t.Errorf("selected row = %q, want %q", got, two)
	}

	// The same row again closes it, and the row has the keyboard.
	p.eval(rowOf(two)+`.click()`, nil)
	p.until("closed by the same row", `document.getElementById('detail').hidden && !document.querySelector('.task.row-open')`)

	// × closes it.
	p.eval(rowOf(one)+`.click()`, nil)
	p.until("open again", isOpen(one))
	p.eval(`document.getElementById('detail-close').click()`, nil)
	p.until("closed by ×", `document.getElementById('detail').hidden`)
	p.until("the row has the focus", `document.activeElement === `+rowOf(one))

	// Enter on the focused row opens it with the focus on the panel; Escape
	// closes it and returns the focus to the row.
	p.key(kb.Enter)
	p.until("opened by Enter", isOpen(one))
	p.until("the panel has the focus", `document.activeElement === document.getElementById('detail')`)
	p.esc()
	p.until("closed by Escape", `document.getElementById('detail').hidden`)
	p.until("the row has the focus again", `document.activeElement === `+rowOf(one))
}

// The panel is full width below 900px, and a link to #T001 opens it.
func TestTodoDetailPanelHashAndNarrow(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "first")
	id := a.addTask(t, "second")
	p := newPage(t, a, "/#"+id)
	p.until("opened by the link", panelOpenFor+`('`+id+`')`)
	if got := p.str(`document.querySelector('.task.row-open').dataset.id`); got != id {
		t.Errorf("selected row = %q, want %q", got, id)
	}

	p.run(chromedp.EmulateViewport(700, 900))
	p.until("full width", `Math.abs(document.getElementById('detail').getBoundingClientRect().width - window.innerWidth) < 1`)
	if got := p.num(`document.documentElement.scrollWidth - document.documentElement.clientWidth`); got > 1 {
		t.Errorf("the page scrolls sideways by %v px", got)
	}
	p.run(chromedp.EmulateViewport(1500, 900))
	p.until("480px wide at 1440 and over", `Math.abs(document.getElementById('detail').getBoundingClientRect().width - 480) < 1`)
}

// An open task that leaves the board closes the panel; one that only moves to
// 今週完了 keeps it, so the completion can be taken back.
func TestTodoDetailPanelFollowsTheBoard(t *testing.T) {
	a := startApp(t)
	gone := a.addTask(t, "to delete")
	doneOne := a.addTask(t, "to complete")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)

	p.openRow(doneOne)
	a.done(t, doneOne)
	p.reload()
	p.until("the completed task is still shown", `document.querySelector('.d-complete') && document.querySelector('.d-complete').textContent === '完了を取り消す'`)
	if got := p.str(`String(document.getElementById('detail').hidden)`); got != "false" {
		t.Errorf("panel hidden after the task was completed")
	}

	p.openRow(gone)
	p.eval(`(fetch('/api/tasks/`+gone+`', {method: 'DELETE'}), true)`, nil)
	// Each try reads the list again, as the poll does.
	p.until("the panel closed with the task", `(document.dispatchEvent(new Event('visibilitychange')), document.getElementById('detail').hidden)`)
}
