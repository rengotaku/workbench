//go:build e2e

package e2e

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

const queueNames = `Array.from(document.querySelectorAll('#queue .task')).map(e => e.querySelector('.txt').textContent.trim())`

// queueHas is a JS expression: the queue has n rows and one of them is name.
func queueHas(n int, name string) string {
	return fmt.Sprintf(`document.querySelectorAll('#queue .task').length === %d && %s.includes('%s')`, n, queueNames, name)
}

// A list reply that arrives late, after a newer read has drawn, must not draw
// over it. The reply to the first read is held back while a task is added and a
// newer read draws, then let through.
func TestTodoListLateReplyDoesNotOverwriteNewerList(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "first")
	p := newPage(t, a, "/")
	p.until("first drawn", `document.querySelectorAll('#queue .task').length === 1`)

	g := p.holdList(fetch.RequestStageResponse)
	// Read 0: coming back to the window reads again. The server answers with
	// just "first"; the reply is held at the page's door.
	p.eval(`document.dispatchEvent(new Event('visibilitychange'))`, nil)
	g.waitHeld(t, 5*time.Second)

	a.addTask(t, "second")
	p.reload() // read 1: sees both, draws at once
	p.until("both drawn", `document.querySelectorAll('#queue .task').length === 2`)

	g.release()
	p.until("the late reply handled", `window.__listHandled[0] === true`)

	var got []string
	p.eval(queueNames, &got)
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("queue after the late reply = %v, want %v (the old reply drew over the new one)", got, want)
	}
}

// A row keeps its element across reads: with an unrelated change above and
// below it, the same <li> is still there (keyed by the task's id), not another
// task's data poured into it.
func TestTodoListRowsKeepTheirElementAcrossReloads(t *testing.T) {
	a := startApp(t)
	first := a.addTask(t, "first")
	a.addTask(t, "second")
	a.addTask(t, "third")
	p := newPage(t, a, "/")
	p.until("3 rows", `document.querySelectorAll('#queue .task').length === 3`)
	p.eval(`document.querySelectorAll('#queue .task').forEach(li => { li.__mark = li.dataset.id; })`, nil)

	// The first row goes away and a new one comes at the end: every row
	// below the first moves up by one place.
	a.done(t, first)
	a.addTask(t, "fourth")
	p.reload()
	p.until("redrawn", queueHas(3, "fourth"))

	var got []string
	p.eval(`Array.from(document.querySelectorAll('#queue .task')).map(li => li.querySelector('.txt').textContent.trim() + ':' + (li.__mark === li.dataset.id ? 'same' : li.__mark === undefined ? 'new' : 'other'))`, &got)
	if want := []string{"second:same", "third:same", "fourth:new"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// openSecond seeds three tasks, opens the second row and returns the ids.
func openSecond(t *testing.T, a *app) (first, second string, p *page) {
	t.Helper()
	first = a.addTask(t, "first")
	second = a.addTask(t, "second")
	a.addTask(t, "third")
	p = newPage(t, a, "/")
	p.until("3 rows", `document.querySelectorAll('#queue .task').length === 3`)
	p.eval(`document.querySelectorAll('#queue .task')[1].click()`, nil)
	p.until("second open", `document.querySelectorAll('.task.row-open').length === 1 && document.querySelector('#detail .d-name .txt.live')`)
	return first, second, p
}

// #115: the poll's list reply arrives while a field of the open row is being
// typed in. The field stays the same node with what was typed, the rows around
// it follow the new list, and the poll goes on afterwards.
func TestTodoListPollReplyKeepsTheStatusFieldBeingTyped(t *testing.T) {
	a := startApp(t)
	first, second, p := openSecond(t, a)
	field := `document.getElementById('status-` + second + `')`
	p.until("status field", field)
	p.eval(field+`.__mark = 'same'`, nil)

	// A change elsewhere moves the revision, so the next poll reads the list;
	// that read is held while the field is typed in.
	g := p.holdList(fetch.RequestStageRequest)
	a.addTask(t, "fourth")
	g.waitHeld(t, 10*time.Second)
	p.run(chromedp.SendKeys("#status-"+second, "draft text", chromedp.ByQuery))
	a.done(t, first)
	g.release()
	p.until("list redrawn", queueHas(3, "fourth"))

	if got := p.str(field + `.__mark || ''`); got != "same" {
		t.Error("the status field was replaced by the redraw")
	}
	if got := p.str(field + `.value`); got != "draft text" {
		t.Errorf("typed text = %q, want it kept", got)
	}
	if got := p.str(`String(document.activeElement === ` + field + `)`); got != "true" {
		t.Error("the field lost the focus")
	}

	// Leaving the field lets the poll run again: a change made now shows up
	// without anyone touching the page.
	p.eval(`document.activeElement.blur()`, nil)
	a.addTask(t, "fifth")
	p.untilWithin(12*time.Second, "the poll to bring fifth", queueNames+`.includes('fifth')`)
}

// #115 for the name: the open row's name stays the same element with what was
// typed in it when a reply redraws the list around it.
func TestTodoListPollReplyKeepsTheNameBeingTyped(t *testing.T) {
	a := startApp(t)
	first, _, p := openSecond(t, a)
	name := `document.querySelector('#detail .d-name .txt.live')`
	p.eval(name+`.__mark = 'same'`, nil)

	g := p.holdList(fetch.RequestStageRequest)
	a.addTask(t, "fourth")
	g.waitHeld(t, 10*time.Second)
	p.run(chromedp.SendKeys(`#detail .d-name .txt.live`, "XYZ", chromedp.ByQuery))
	// The row above the open one goes: without keys by task id, the open
	// row's element would be taken over by its neighbour.
	a.done(t, first)
	g.release()
	p.until("list redrawn", queueHas(3, "fourth"))

	if got := p.str(name + `.__mark || ''`); got != "same" {
		t.Error("the name field was replaced by the redraw")
	}
	if got := p.str(name + `.textContent`); !strings.Contains(got, "XYZ") || !strings.Contains(got, "second") {
		t.Errorf("name text = %q, want the typed XYZ kept with second", got)
	}
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Errorf("open rows = %v, want 1", n)
	}
}

// #115: a reply that takes the focused field away (its task finished
// elsewhere) must not leave the poll held: the browser may send no blur for an
// element that is removed.
func TestTodoListFieldRemovedWhileFocusedReleasesThePoll(t *testing.T) {
	a := startApp(t)
	_, second, p := openSecond(t, a)
	field := `document.getElementById('status-` + second + `')`
	p.until("status field", field)

	g := p.holdList(fetch.RequestStageRequest)
	a.addTask(t, "fourth")
	g.waitHeld(t, 10*time.Second)
	p.run(chromedp.SendKeys("#status-"+second, "draft", chromedp.ByQuery))
	// Some browsers send no blur when the focused element is removed. Swallow
	// blur from here on so this browser behaves that way too.
	p.eval(`(() => {
		for (const type of ['blur', 'focusout']) {
			window.addEventListener(type, ev => ev.stopImmediatePropagation(), true);
		}
		return true;
	})()`, nil)
	a.remove(t, second) // the open task is gone: the panel closes with it
	g.release()
	p.until("the open row gone", `document.querySelectorAll('.task.row-open').length === 0 && `+queueNames+`.includes('fourth')`)
	if got := p.str(`String(!!document.getElementById('status-` + second + `'))`); got != "false" {
		t.Fatal("the field is still on the page")
	}

	a.addTask(t, "fifth")
	p.untilWithin(12*time.Second, "the poll to bring fifth", queueNames+`.includes('fifth')`)
}

// What a reply changed while a field held the open row is taken up once the
// field is left, with no further change on the server: the panel must not stay
// a copy of the old task (editing it would send the old related URLs back).
func TestTodoListHeldPanelCatchesUpWhenTheFieldIsLeft(t *testing.T) {
	a := startApp(t)
	_, second, p := openSecond(t, a)
	p.until("status field", `document.getElementById('status-`+second+`')`)

	g := p.holdList(fetch.RequestStageRequest)
	a.addTask(t, "fourth")
	g.waitHeld(t, 10*time.Second)
	p.run(chromedp.SendKeys("#status-"+second, "draft", chromedp.ByQuery))
	a.patch(t, second, map[string]any{"links": []map[string]any{{"url": "https://example.com/added", "label": "added"}}})
	g.release()
	p.until("list redrawn", queueHas(4, "fourth"))
	if n := p.num(`document.querySelectorAll('#detail .link-row').length`); n != 0 {
		t.Fatalf("link rows while the field is held = %v, want the panel kept as it was", n)
	}

	p.eval(`document.activeElement.blur()`, nil)
	p.until("the added URL shown", `document.querySelectorAll('#detail .link-row').length === 1`)
}

// 見直した is drawn disabled while its request is out and enabled again after
// the reload. A long-run task with no next step stays in the band (no_next).
func TestTodoListReviewButtonIsEnabledAgainAfterReload(t *testing.T) {
	a := startApp(t)
	id := a.addTask(t, "longone")
	a.post(t, "/api/tasks/"+id+"/long", map[string]any{"on": true})
	p := newPage(t, a, "/")
	btn := `#review-list .rv-done`
	p.until("in the review band", `document.querySelector('`+btn+`')`)
	p.click(btn)
	p.until("reviewed", `document.getElementById('err').textContent.includes('見直した')`)
	if got := p.str(`String(!!document.querySelector('` + btn + `') && document.querySelector('` + btn + `').disabled)`); got != "false" {
		t.Errorf("見直した present and disabled = %s after the reload, want present and enabled", got)
	}
	if n := p.num(`document.querySelectorAll('` + btn + `').length`); n != 1 {
		t.Errorf("review band buttons = %v, want the task to stay (no next step)", n)
	}
}
