//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// openDropAskFor opens the reason field of an old task (the page is moved two
// hours on, so the trash asks).
func openDropAskFor(t *testing.T, p *page, id string) {
	t.Helper()
	p.fakeTwoHoursLater()
	del := `document.querySelector('.task[data-id="` + id + `"] .del')`
	p.until("trash asks", del+`.title === '理由を書いて削除する'`)
	p.eval(del+`.click()`, nil)
	p.until("ask focused", `document.activeElement === document.querySelector('.drop-ask-input')`)
}

const whoField = `.drop-ask input[aria-label="渡した相手"]`

// The wait button hands the task on instead of deleting it; the written reason
// becomes the wait note, and the row closes with the usual notice.
func TestTodoDropAskWait(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	review := a.addTask(t, "review")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	openDropAskFor(t, p, review)

	p.run(chromedp.SendKeys(`.drop-ask-input`, "高橋さんが行っている", chromedp.ByQuery))
	p.run(chromedp.SendKeys(whoField, "高橋", chromedp.ByQuery))
	p.click(`.drop-ask-wait`)
	p.until("moved, ask closed", `document.querySelectorAll('#queue .task').length === 1 && !document.querySelector('.drop-ask')`)
	p.until("notice", `(`+noticeText+`).startsWith('待ちへ移した')`)

	got := a.task(t, review)
	if got == nil {
		t.Fatal("the task was deleted")
	}
	if str(got, "state") != "waiting" || str(got, "handed_to") != "高橋" || str(got, "wait_note") != "高橋さんが行っている" {
		t.Errorf("state=%s handed_to=%s wait_note=%s, want waiting 高橋 高橋さんが行っている",
			str(got, "state"), str(got, "handed_to"), str(got, "wait_note"))
	}
	if dropped, _ := a.board(t, "all")["dropped"].([]any); len(dropped) != 0 {
		t.Errorf("dropped = %v, want none", dropped)
	}
}

// Enter in the name field waits (with no reason the note is empty).
func TestTodoDropAskEnterInNameWaits(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	review := a.addTask(t, "review")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	openDropAskFor(t, p, review)

	p.run(chromedp.SendKeys(whoField, "bob", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("moved", `document.querySelectorAll('#queue .task').length === 1 && !document.querySelector('.drop-ask')`)
	got := a.task(t, review)
	if got == nil || str(got, "state") != "waiting" || str(got, "handed_to") != "bob" || str(got, "wait_note") != "" {
		t.Errorf("task = %v, want waiting bob with no note", got)
	}
}

// An empty (or blank) name sends nothing and puts the caret back in the name field.
func TestTodoDropAskWaitNeedsAName(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	review := a.addTask(t, "review")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	openDropAskFor(t, p, review)

	p.eval(`window.__waits = 0; const f = window.fetch; window.fetch = (u, o) => { if (String(u).includes('/wait')) window.__waits++; return f(u, o); }; true`, nil)
	p.run(chromedp.SendKeys(`.drop-ask-input`, "a reason", chromedp.ByQuery))
	p.click(`.drop-ask-wait`)
	p.until("focus back on the name", `document.activeElement === document.querySelector('`+whoField+`')`)
	p.run(chromedp.SendKeys(whoField, "   ", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("still on the name", `document.activeElement === document.querySelector('`+whoField+`')`)
	time.Sleep(300 * time.Millisecond)
	if n := p.num(`window.__waits`); n != 0 {
		t.Errorf("%v wait requests sent for an empty name, want none", n)
	}
	if got := a.task(t, review); got == nil || str(got, "state") == "waiting" {
		t.Errorf("task = %v, want untouched", got)
	}
	if n := p.num(`document.querySelectorAll('.drop-ask').length`); n != 1 {
		t.Errorf("the ask closed")
	}
}

// Tab walks reason, delete, name, wait; Escape from the name field closes and
// returns the focus to the trash.
func TestTodoDropAskTabOrderAndEscape(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	review := a.addTask(t, "review")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	openDropAskFor(t, p, review)

	p.run(chromedp.SendKeys(`.drop-ask-input`, "r", chromedp.ByQuery))
	focused := `document.activeElement.className + '|' + document.activeElement.textContent`
	var got string
	for _, want := range []string{"drop-ask-go|削除する", "drop-ask-input|", "drop-ask-wait|待ちにする"} {
		p.key(kb.Tab)
		p.eval(focused, &got)
		if got != want {
			t.Fatalf("tab stop = %q, want %q", got, want)
		}
	}
	p.eval(`document.querySelector('`+whoField+`').focus(); true`, nil)
	p.esc()
	del := `document.querySelector('.task[data-id="` + review + `"] .del')`
	p.until("closed, trash focused", `!document.querySelector('.drop-ask') && document.activeElement === `+del)
	if got := a.task(t, review); got == nil || str(got, "state") == "waiting" {
		t.Error("Escape changed the task")
	}
}

// The name being typed survives a list reply: same element, same text, same focus.
func TestTodoDropAskNameSurvivesAListReply(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	gone := a.addTask(t, "gone")
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "x"})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	p.click(`#wait-fold`)
	p.openRow(waiter)
	openDropAskFor(t, p, gone)

	p.eval(`window.__who = document.querySelector('`+whoField+`'); window.__who.focus(); true`, nil)
	p.run(chromedp.SendKeys(whoField, "taka", chromedp.ByQuery))
	p.clickButton(waiter, "着手順へ戻す")
	p.until("list reply drawn", `document.querySelectorAll('#queue .task').length === 3 && document.querySelectorAll('.task.landed').length === 1`)
	p.until("same field, same text, still focused", `window.__who === document.querySelector('`+whoField+`') && window.__who.value === 'taka' && document.activeElement === window.__who`)
}
