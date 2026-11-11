//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The list is a table: one column-heading line, and each row has a cell for
// 期限 / 話題 / 止まって / 状況. A queue row with no due date warns in its 期限
// cell, a waiting row says how long the ball has been away instead of a date.
func TestTodoTableCells(t *testing.T) {
	a := startApp(t)
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	open := a.addTask(t, "no date")
	dated := a.addTaskDue(t, "dated", tomorrow)
	focus := a.addTask(t, "focused")
	a.post(t, "/api/tasks/"+focus+"/focus", map[string]any{"on": true})
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "x"})
	a.patch(t, open, map[string]any{"topic": "話題A"})
	a.post(t, "/api/tasks/"+open+"/annotations", map[string]any{"kind": "memo", "body": "最新の補足です"})

	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)

	cell := func(id, sel string) string {
		return p.str(`(document.querySelector('.task[data-id="` + id + `"] ` + sel + `') || {textContent: '-'}).textContent.trim()`)
	}

	// One heading line for the whole list, with the five columns.
	if n := p.num(`document.querySelectorAll('.tbl-colhead').length`); n != 1 {
		t.Errorf("column heading lines = %v, want 1", n)
	}
	if got := p.str(`Array.from(document.querySelectorAll('.tbl-colhead span')).map(s => s.textContent).join(',')`); got != "タスク名,期限,話題,止まって,状況," {
		t.Errorf("column headings = %q", got)
	}

	// A queue row with no date warns; a dated one and a focused one do not.
	if got := cell(open, ".due"); got != "⚠ 期限なし" {
		t.Errorf("open-ended queue row due cell = %q, want the warning", got)
	}
	if n := p.num(`document.querySelectorAll('.task[data-id="` + open + `"] button.due-warn').length`); n != 1 {
		t.Errorf("warning is not a due-menu button: %v", n)
	}
	if got := cell(dated, ".due"); got != "明日 "+time.Now().AddDate(0, 0, 1).Format("1/2") {
		t.Errorf("dated row due cell = %q", got)
	}
	if n := p.num(`document.querySelectorAll('.due-warn').length`); n != 1 {
		t.Errorf("warnings on the screen = %v, want only the open-ended queue row", n)
	}
	// Pressing the warning opens the due menu.
	p.click(`.task[data-id="` + open + `"] .due-warn`)
	p.until("due menu open", `!!document.querySelector('.due-menu')`)
	p.esc()
	p.until("due menu closed", `!document.querySelector('.due-menu')`)

	// The topic tag and the latest supplement.
	if got := cell(open, ".topic-tag"); got != "話題A" {
		t.Errorf("topic cell = %q", got)
	}
	if got := cell(open, ".status-text"); got != "最新の補足です" {
		t.Errorf("status cell = %q", got)
	}

	// A waiting row has no date and no warning, and says how long it has waited.
	p.click(`#wait-fold`)
	p.until("waiting row", `document.querySelectorAll('#waitlist .task').length === 1`)
	if got := cell(waiter, ".due"); got != "渡して 0日" {
		t.Errorf("waiting row due cell = %q, want 渡して 0日", got)
	}
	if n := p.num(`document.querySelectorAll('#waitlist .due-warn, #waitlist .due-edit').length`); n != 0 {
		t.Errorf("waiting row has a due warning or chip: %v", n)
	}

	// A 390px window: the table scrolls inside its box and the page does not.
	p.run(chromedp.EmulateViewport(390, 800))
	p.until("the box scrolls", `(() => { const b = document.querySelector('.tbl-scroll'); return b.scrollWidth > b.clientWidth; })()`)
	if got := p.num(`document.documentElement.scrollWidth - document.documentElement.clientWidth`); got > 1 {
		t.Errorf("the page scrolls sideways by %v px at 390px wide", got)
	}
	// The status column keeps a readable width; the box scroll reaches it.
	if w := p.num(`document.querySelector('.task[data-id="` + open + `"] .c-status').getBoundingClientRect().width`); w < 100 {
		t.Errorf("status column is %v px wide, want >= 100", w)
	}

	// A menu opened on a row closes when the table scrolls sideways, rather
	// than staying where the row was.
	// Click from script: chromedp's Click scrolls the box to the button first,
	// and that scroll would itself close the menu this step opens.
	p.eval(`document.querySelector('.task[data-id="`+open+`"] .due-warn').click(), true`, nil)
	p.until("due menu open again", `!!document.querySelector('.due-menu')`)
	p.eval(`(() => { const b = document.querySelector('.tbl-scroll'); b.scrollLeft = 200; b.dispatchEvent(new Event('scroll')); return true; })()`, nil)
	p.until("due menu closed on scroll", `!document.querySelector('.due-menu')`)
}
