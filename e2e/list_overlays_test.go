//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// board reads the whole list from the API, for a scope ("all", "week").
func (a *app) board(t *testing.T, scope string) map[string]any {
	t.Helper()
	res, err := http.Get(a.base + "/api/tasks?scope=" + scope)
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var board map[string]any
	if err := json.NewDecoder(res.Body).Decode(&board); err != nil {
		t.Fatalf("GET /api/tasks: bad JSON: %v", err)
	}
	return board
}

// queueOrder is the names of the queue, in the server's order.
func (a *app) queueOrder(t *testing.T) []string {
	t.Helper()
	var names []string
	rows, _ := a.board(t, "all")["queue"].([]any)
	for _, raw := range rows {
		if task, ok := raw.(map[string]any); ok {
			names = append(names, str(task, "name"))
		}
	}
	return names
}

func day(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }

// chipOf is a JS expression for the due chip of a task's row.
func chipOf(id string) string {
	return `document.querySelector('.task[data-id="` + id + `"] .due-edit')`
}

// openMenu opens a row's due menu as a click on its chip does.
func (p *page) openMenu(id string) {
	p.t.Helper()
	p.until("chip "+id, chipOf(id))
	p.eval(chipOf(id)+`.click()`, nil)
	p.until("menu open", `document.querySelector('.due-menu')`)
}

// pickItem presses the menu item whose text starts with label.
func (p *page) pickItem(label string) {
	p.t.Helper()
	p.eval(`Array.from(document.querySelectorAll('.due-menu .dm-item')).find(b => b.textContent.startsWith('`+label+`')).click()`, nil)
}

// pickDay presses a day (YYYY-MM-DD) of the open menu's calendar, going on to
// the next month first when the month shown does not hold it.
func (p *page) pickDay(value string) {
	p.t.Helper()
	sel := `.due-menu .cal-day[data-day="` + value + `"]`
	var have bool
	p.eval(`!!document.querySelector('`+sel+`')`, &have)
	if !have {
		p.eval(`document.querySelector('.due-menu .cal-nav button[aria-label="次の月"]').click()`, nil)
		p.until("month of "+value, `!!document.querySelector('`+sel+`')`)
	}
	p.eval(`document.querySelector('`+sel+`').click()`, nil)
}

// noticeText is what the notice says, "" while it is hidden.
const noticeText = `(e => e.hidden ? '' : e.textContent.replace(/\s+/g, ' ').trim())(document.getElementById('err'))`

// The due menu: each way of moving a due date sets it, 期限なし clears it,
// いつかにする shelves the task, and the notice that follows can take it back.
func TestTodoDueMenu(t *testing.T) {
	a := startApp(t)
	week := a.addTask(t, "week")
	two := a.addTask(t, "two")
	month := a.addTask(t, "month")
	picked := a.addTask(t, "picked")
	clear := a.addTaskDue(t, "clear", day(2))
	shelved := a.addTask(t, "shelved")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 6`)

	today := time.Now()
	p.openMenu(week)
	if lbl := p.str(`document.querySelector('.due-menu').getAttribute('aria-label')`); lbl != "期限" {
		t.Errorf("menu label = %q", lbl)
	}
	p.pickItem("+1週間")
	a.untilTask(t, week, "due +1 week", func(k map[string]any) bool { return str(k, "due") == day(7) })
	p.until("menu closed", `!document.querySelector('.due-menu')`)
	p.until("notice", `(`+noticeText+`).includes('ずらした') && document.querySelectorAll('#err .undo-link').length === 1`)

	p.openMenu(two)
	p.pickItem("+2週間")
	a.untilTask(t, two, "due +2 weeks", func(k map[string]any) bool { return str(k, "due") == day(14) })

	p.openMenu(month)
	p.pickItem("+1ヶ月")
	want := today.AddDate(0, 1, 0).Format("2006-01-02")
	a.untilTask(t, month, "due +1 month", func(k map[string]any) bool { return str(k, "due") == want })

	p.openMenu(picked)
	p.pickDay(day(5))
	a.untilTask(t, picked, "due picked", func(k map[string]any) bool { return str(k, "due") == day(5) })
	p.until("notice for the date", `(`+noticeText+`).startsWith('期限を')`)

	p.openMenu(clear)
	p.pickItem("期限なし")
	a.untilTask(t, clear, "due cleared", func(k map[string]any) bool { return str(k, "due") == "" })
	p.until("cleared notice", `(`+noticeText+`).startsWith('期限を外した')`)

	// The notice offers to take it back; pressing it puts the date back.
	p.eval(`document.querySelector('#err .undo-link').click()`, nil)
	a.untilTask(t, clear, "due restored", func(k map[string]any) bool { return str(k, "due") == day(2) })

	p.openMenu(shelved)
	p.pickItem("いつかにする")
	a.untilTask(t, shelved, "shelved", func(k map[string]any) bool { return k["until"] != nil })
	p.until("shelved gone from the queue", `!document.querySelector('#queue .task[data-id="`+shelved+`"]')`)

	// The notice goes by itself.
	p.untilWithin(9*time.Second, "notice gone", `(`+noticeText+`) === ''`)
}

// The menu's keys: the calendar's current day (or today) takes the focus, the
// arrows move the day and Enter sets it; outside the calendar the arrows move
// between items; Escape closes it and returns the focus to the chip; and a
// click outside only closes it.
func TestTodoDueMenuKeys(t *testing.T) {
	a := startApp(t)
	id := a.addTaskDue(t, "one", day(2))
	a.addTask(t, "two")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)

	focusedDay := `(document.activeElement.dataset || {}).day`
	p.openMenu(id)
	p.until("the current day focused", focusedDay+` === '`+day(2)+`'`)
	p.key(kb.ArrowRight)
	p.until("a day on", focusedDay+` === '`+day(3)+`'`)
	p.key(kb.ArrowDown)
	p.until("a week on", focusedDay+` === '`+day(10)+`'`)
	p.key(kb.ArrowLeft)
	p.key(kb.ArrowUp)
	p.until("back a week and a day", focusedDay+` === '`+day(2)+`'`)

	// Outside the calendar the arrows move between the items.
	p.eval(`document.querySelector('.due-menu .dm-shift').focus()`, nil)
	p.key(kb.ArrowDown)
	p.until("second shift focused", `document.activeElement.textContent.startsWith('+3日')`)
	p.key(kb.ArrowUp)
	p.until("first shift again", `document.activeElement.textContent.startsWith('+1日')`)

	// Enter on a day sets it and closes the menu.
	p.eval(`document.querySelector('.due-menu .cal-day[tabindex="0"]').focus()`, nil)
	p.key(kb.ArrowLeft)
	p.until("a day back", focusedDay+` === '`+day(1)+`'`)
	p.key(kb.Enter)
	a.untilTask(t, id, "due set by Enter", func(k map[string]any) bool { return str(k, "due") == day(1) })
	p.until("closed by Enter", `!document.querySelector('.due-menu')`)
	p.until("the list has the new date", chipOf(id)+`.dataset.due === '`+day(1)+`'`)

	p.openMenu(id)
	p.esc()
	p.until("closed, the chip has the focus", `!document.querySelector('.due-menu') && document.activeElement === `+chipOf(id))

	// Outside click: closes the menu and does not open the row under it.
	p.openMenu(id)
	p.click(`.app-name`)
	p.until("closed by a click", `!document.querySelector('.due-menu')`)
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 0 {
		t.Errorf("the outside click opened a row (%v open)", n)
	}

	// The menu is at the chip: its right edge under the chip's, 4px below it.
	p.openMenu(id)
	var gap [2]float64
	p.eval(`(() => { const m = document.querySelector('.due-menu').getBoundingClientRect(), c = `+chipOf(id)+`.getBoundingClientRect(); return [Math.round(m.right - c.right), Math.round(m.top - c.bottom)]; })()`, &gap)
	if gap != [2]float64{0, 4} {
		var dbg string
		p.eval(`JSON.stringify([document.querySelector('.due-menu').getBoundingClientRect(), `+chipOf(id)+`.getBoundingClientRect(), window.innerWidth, window.scrollX])`, &dbg)
		t.Errorf("menu offset from the chip (right, below) = %v, want [0 4] %s", gap, dbg)
	}
}

// A long-run row's chip opens the review menu: a date, or a step ahead of
// today, posted as the next review day.
func TestTodoReviewMenu(t *testing.T) {
	a := startApp(t)
	long := a.addTask(t, "longone")
	a.post(t, "/api/tasks/"+long+"/long", map[string]any{"on": true})
	p := newPage(t, a, "/")
	p.click(`#long-fold`)
	p.until("long open", `!document.getElementById('long-list').hidden`)

	chip := `#long-list .task[data-id="` + long + `"] .review-chip`
	p.eval(`document.querySelector('`+chip+`').click()`, nil)
	p.until("review menu", `document.querySelector('.due-menu') && document.querySelector('.due-menu').getAttribute('aria-label') === '次に見る日'`)
	if n := p.num(`document.querySelectorAll('.due-menu .dm-item').length`); n != 3 {
		t.Errorf("review menu items = %v, want 3 (no clear, no someday)", n)
	}
	p.pickItem("+2週間")
	a.untilTask(t, long, "review in 2 weeks", func(k map[string]any) bool { return str(k, "review_on") == day(14) })
	p.until("notice", `(`+noticeText+`).startsWith('次に見る日を')`)

	p.eval(`document.querySelector('`+chip+`').click()`, nil)
	p.until("review menu again", `document.querySelector('.due-menu')`)
	p.pickDay(day(3))
	a.untilTask(t, long, "review on a date", func(k map[string]any) bool { return str(k, "review_on") == day(3) })
}

// fakeTwoHoursLater makes the page see every task as older than the hour after
// which deleting asks for a reason (ADR todo-0016). The age is judged in the
// page from created_at; the server has no way to back-date a task.
func (p *page) fakeTwoHoursLater() {
	p.t.Helper()
	p.eval(`(() => { const now = Date.now; Date.now = () => now() + 2 * 3600 * 1000; return true; })()`, nil)
	p.reload()
}

// Deleting a fresh task asks nothing, and the notice undoes it.
func TestTodoDropWithoutReasonAndUndo(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	gone := a.addTask(t, "gone")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)

	p.eval(`document.querySelector('.task[data-id="`+gone+`"] .del').click()`, nil)
	p.until("deleted", `document.querySelectorAll('#queue .task').length === 1`)
	if n := p.num(`document.querySelectorAll('.drop-ask').length`); n != 0 {
		t.Errorf("a fresh task asked for a reason")
	}
	p.until("notice with undo", `(`+noticeText+`).startsWith('削除した: gone') && document.querySelectorAll('#err .undo-link').length === 1`)

	p.eval(`document.querySelector('#err .undo-link').click()`, nil)
	p.until("restored", `document.querySelectorAll('#queue .task').length === 2`)
	if got := a.task(t, gone); got == nil {
		t.Error("undo did not bring the task back")
	}
	p.until("undone notice, no undo of an undo", `(`+noticeText+`) !== '' && document.querySelectorAll('#err .undo-link').length === 0`)
	p.untilWithin(9*time.Second, "notice gone", `(`+noticeText+`) === ''`)
}

// Deleting a task older than an hour asks why first. Escape leaves the task
// and returns the focus to the trash; Enter with a reason deletes, records the
// reason and the notice undoes it.
func TestTodoDropWithReason(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	gone := a.addTask(t, "gone")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	p.fakeTwoHoursLater()

	del := `document.querySelector('.task[data-id="` + gone + `"] .del')`
	p.until("trash asks", del+`.title === '理由を書いて削除する'`)
	p.eval(del+`.click()`, nil)
	p.until("ask open and focused", `document.querySelector('.drop-ask') && document.activeElement === document.querySelector('.drop-ask-input')`)
	p.until("delete button waits for a reason", `document.querySelector('.drop-ask-go').disabled`)
	p.esc()
	p.until("closed, trash focused", `!document.querySelector('.drop-ask') && document.activeElement === `+del)
	if a.task(t, gone) == nil {
		t.Fatal("Escape deleted the task")
	}

	p.eval(del+`.click()`, nil)
	p.until("ask open again", `document.activeElement === document.querySelector('.drop-ask-input')`)
	p.run(chromedp.SendKeys(`.drop-ask-input`, "no longer needed", chromedp.ByQuery))
	p.until("delete button enabled", `!document.querySelector('.drop-ask-go').disabled`)
	p.key(kb.Enter)
	p.until("deleted", `document.querySelectorAll('#queue .task').length === 1 && !document.querySelector('.drop-ask')`)

	dropped, _ := a.board(t, "all")["dropped"].([]any)
	if len(dropped) != 1 {
		t.Fatalf("dropped = %v, want one", dropped)
	}
	if first, _ := dropped[0].(map[string]any); str(first, "reason") != "no longer needed" {
		t.Errorf("dropped = %v, want the reason", dropped)
	}
	p.until("notice with undo", `(`+noticeText+`).startsWith('削除した: gone')`)
	p.eval(`document.querySelector('#err .undo-link').click()`, nil)
	p.until("restored", `document.querySelectorAll('#queue .task').length === 2`)
}

// The reason being typed survives a list reply: the field is the same element,
// keeps its text and its focus (the draw does not re-mount it).
func TestTodoDropReasonSurvivesAListReply(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "keep")
	gone := a.addTask(t, "gone")
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "x"})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)
	p.click(`#wait-fold`)
	p.openRow(waiter)
	p.fakeTwoHoursLater()

	p.until("trash asks", `document.querySelector('.task[data-id="`+gone+`"] .del').title === '理由を書いて削除する'`)
	p.eval(`document.querySelector('.task[data-id="`+gone+`"] .del').click()`, nil)
	p.until("ask focused", `document.activeElement === document.querySelector('.drop-ask-input')`)
	p.eval(`window.__ask = document.querySelector('.drop-ask-input'); true`, nil)
	p.run(chromedp.SendKeys(`.drop-ask-input`, "still typing", chromedp.ByQuery))

	// A change from the panel of another row: no mousedown, so the field is not
	// left; it brings a list reply, a notice and a tint on the row.
	p.clickButton(waiter, "着手順へ戻す")
	p.until("list reply drawn", `document.querySelectorAll('#queue .task').length === 3 && document.querySelectorAll('.task.landed').length === 1`)

	p.until("same field, same text, still focused", `window.__ask === document.querySelector('.drop-ask-input') && window.__ask.value === 'still typing' && document.activeElement === window.__ask`)
	p.untilWithin(12*time.Second, "tint gone", `document.querySelectorAll('.task.landed').length === 0`)
	p.until("same field after the tint's draw", `window.__ask === document.querySelector('.drop-ask-input') && window.__ask.value === 'still typing'`)
}

// startDrag, dragOver and finishDrag play one drag in three steps, so a test
// can do something between them. The DataTransfer is kept on the window.
func (p *page) startDrag(src string) {
	p.t.Helper()
	p.eval(`(() => { window.__dt = new DataTransfer(); document.querySelector(`+quote(src)+`).dispatchEvent(new DragEvent('dragstart', {bubbles: true, cancelable: true, dataTransfer: window.__dt})); return true; })()`, nil)
}

func (p *page) dragOver(dst string) {
	p.t.Helper()
	p.eval(`(() => { const d = document.querySelector(`+quote(dst)+`), r = d.getBoundingClientRect(); d.dispatchEvent(new DragEvent('dragover', {bubbles: true, cancelable: true, dataTransfer: window.__dt, clientY: r.top + r.height * 0.9})); return true; })()`, nil)
}

func (p *page) finishDrag(src string) {
	p.t.Helper()
	p.eval(`(() => { const s = document.querySelector(`+quote(src)+`); for (const type of ['drop', 'dragend']) s.dispatchEvent(new DragEvent(type, {bubbles: true, cancelable: true, dataTransfer: window.__dt})); return true; })()`, nil)
}

func quote(s string) string { return fmt.Sprintf("%q", s) }

func row(id string) string { return `.task[data-id="` + id + `"]` }

// A drag that puts a row into the band focuses it; one that puts it back into
// the queue unfocuses it.
func TestTodoDragToTheFocusBand(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "one")
	a.addTask(t, "two")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 2`)

	p.startDrag(`#queue ` + row(one))
	p.until("band ready, row dimmed", `document.getElementById('focus-band').classList.contains('drop-ready') && document.querySelector('`+row(one)+`').classList.contains('dragging')`)
	p.dragOver(`#focus-band`)
	p.until("preview in the band", `document.querySelector('#focus-list `+row(one)+`')`)
	p.finishDrag(`#focus-list ` + row(one))
	a.untilTask(t, one, "focused", func(k map[string]any) bool { return k["focused"] == true })
	p.until("drawn in the band", `document.querySelector('#focus-list `+row(one)+`') && !document.getElementById('focus-band').classList.contains('drop-ready') && !document.querySelector('.task.dragging')`)
	p.until("notice", `(`+noticeText+`) .startsWith('フォーカスに入れた')`)

	p.startDrag(`#focus-list ` + row(one))
	p.dragOver(`#queue`)
	p.finishDrag(`#queue ` + row(one))
	a.untilTask(t, one, "unfocused", func(k map[string]any) bool { return k["focused"] != true })
	p.until("back in the queue", `document.querySelector('#queue `+row(one)+`')`)
}

// A drag across the scope's edge changes the due date so the row stays where it
// was put: out of scope takes the first day past it, back in takes the last day.
func TestTodoDragAcrossSections(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "one")
	far := a.addTaskDue(t, "far", day(60))
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1 && document.querySelectorAll('#outlist .task').length === 1`)
	p.click(`#out-fold`)
	p.until("out open", `!document.getElementById('outlist').hidden`)

	board := a.board(t, "week") // the page opens on the week
	days, ok := board["scope_days"].(float64)
	if !ok {
		t.Fatalf("no scope_days in the board: %v", board["scope_days"])
	}
	scope := int(days)
	today, err := time.Parse("2006-01-02", str(board, "today"))
	if err != nil {
		t.Fatal(err)
	}
	past := today.AddDate(0, 0, scope+1).Format("2006-01-02")
	last := today.AddDate(0, 0, scope).Format("2006-01-02")

	p.startDrag(`#queue ` + row(one))
	p.dragOver(`#outlist ` + row(far))
	p.finishDrag(`#outlist ` + row(one))
	a.untilTask(t, one, "out of scope", func(k map[string]any) bool { return str(k, "due") == past })
	p.until("drawn in 外", `document.querySelector('#outlist `+row(one)+`')`)

	p.startDrag(`#outlist ` + row(far))
	p.dragOver(`#queue`)
	p.finishDrag(`#queue ` + row(far))
	a.untilTask(t, far, "back in scope", func(k map[string]any) bool { return str(k, "due") == last })
	p.until("drawn in the queue", `document.querySelector('#queue `+row(far)+`')`)
}

// A draw in the middle of a drag (here the reply to a change made from another
// row's panel, then the tint's timer) does not change where the drop lands: the
// destination is held in the page's state, not read from the moved element.
func TestTodoDragSurvivesADrawInTheMiddle(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "one")
	a.addTask(t, "two")
	three := a.addTask(t, "three")
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "x"})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 3`)
	p.click(`#wait-fold`)
	p.openRow(waiter)

	p.startDrag(`#queue ` + row(one))
	p.dragOver(`#queue ` + row(three))
	p.until("preview moved", queueNames+`.join() === 'two,three,one'`)

	// The waiter comes back to the queue: the list is read again and drawn
	// while the drag is on.
	p.clickButton(waiter, "着手順へ戻す")
	p.until("drawn mid-drag", `document.querySelectorAll('#queue .task').length === 4 && document.querySelectorAll('.task.landed').length === 1`)
	var names []string
	p.eval(queueNames, &names)
	if want := []string{"two", "three", "one", "waiter"}; !reflect.DeepEqual(names, want) {
		t.Errorf("preview after the mid-drag draw = %v, want %v", names, want)
	}
	// And once more, when the tint's timer draws.
	p.untilWithin(12*time.Second, "tint gone", `document.querySelectorAll('.task.landed').length === 0`)

	p.finishDrag(`#queue ` + row(one))
	deadline := time.Now().Add(8 * time.Second)
	want := []string{"two", "three", "one", "waiter"}
	for {
		got := a.queueOrder(t)
		if reflect.DeepEqual(got, want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved order = %v, want %v (the drop landed somewhere else)", strings.Join(got, ","), strings.Join(want, ","))
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.until("drawn in the saved order", queueNames+`.join() === 'two,three,one,waiter'`)
}

// A drop's save is still out when the next drag starts. The first reply must
// not clear the second drag's state: the second drop is still saved.
func TestTodoDragStartedWhileTheLastDropIsSaving(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "one")
	two := a.addTask(t, "two")
	three := a.addTask(t, "three")
	four := a.addTask(t, "four")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 4`)

	// Hold the first reorder request until released.
	p.eval(`(() => {
		const orig = window.fetch;
		window.fetch = function (url, opts) {
			if (String(url).includes('/order') && !window.__held) {
				window.__held = true;
				return new Promise(res => { window.__release = () => res(orig.apply(window, [url, opts])); });
			}
			return orig.apply(this, arguments);
		};
		return true;
	})()`, nil)

	p.dragRow(`#queue `+row(one), `#queue `+row(two), true)
	p.until("first save held", `window.__held === true`)

	// The second drag starts and moves while the first save is out.
	p.startDrag(`#queue ` + row(three))
	p.dragOver(`#queue ` + row(four))
	// (the first row is back home until its reply draws the new order)
	p.until("second preview", queueNames+`.join() === 'one,two,four,three'`)

	p.eval(`window.__release(); true`, nil)
	// The first reply draws; the second drag is still on, its preview kept.
	p.until("second drag survived the reply", queueNames+`.join() === 'two,one,four,three' && document.querySelector('`+row(three)+`').classList.contains('dragging')`)

	p.finishDrag(`#queue ` + row(three))
	deadline := time.Now().Add(8 * time.Second)
	want := []string{"two", "one", "four", "three"}
	for {
		got := a.queueOrder(t)
		if reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved order = %v, want %v (the second drop was lost)", strings.Join(got, ","), strings.Join(want, ","))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The calendar: a grid of days with the current date filled, the shifts of 1
// day and 3 days count from the right base, and the month buttons and the
// arrows move between months.
func TestTodoDueMenuCalendar(t *testing.T) {
	a := startApp(t)
	one := a.addTask(t, "one")
	three := a.addTask(t, "three")
	cur := a.addTaskDue(t, "cur", day(2))
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 3`)

	now := time.Now()
	title := `document.querySelector('.due-menu .cal-title').textContent`
	monthText := func(d time.Time) string { return d.Format("2006") + "年 " + d.Format("1") + "月" }

	p.openMenu(cur)
	if got := p.str(`document.querySelector('.due-menu [role="grid"]').getAttribute('role')`); got != "grid" {
		t.Errorf("role = %q", got)
	}
	if n := p.num(`document.querySelectorAll('.due-menu .cal-day').length`); n != 42 {
		t.Errorf("day buttons = %v, want 6 weeks", n)
	}
	if got := p.str(title); got != monthText(now.AddDate(0, 0, 2)) {
		t.Errorf("month shown = %q, want the month of the due date %q", got, monthText(now.AddDate(0, 0, 2)))
	}
	if got := p.str(`document.querySelector('.due-menu .cal-day.cur').dataset.day`); got != day(2) {
		t.Errorf("filled day = %q, want %q", got, day(2))
	}
	wd := "日月火水木金土"
	want := fmt.Sprintf("%d月%d日（%s）", int(now.AddDate(0, 0, 2).Month()), now.AddDate(0, 0, 2).Day(), string([]rune(wd)[now.AddDate(0, 0, 2).Weekday()]))
	if got := p.str(`document.querySelector('.due-menu .cal-day.cur').getAttribute('aria-label')`); !strings.HasPrefix(got, want) {
		t.Errorf("day label = %q, want it to start with %q", got, want)
	}
	if got := p.str(`document.querySelector('.due-menu .cal-day.today').dataset.day`); got != day(0) {
		t.Errorf("framed day = %q, want today", got)
	}
	if n := p.num(`document.querySelectorAll('.due-menu .dm-shift').length`); n != 5 {
		t.Errorf("shifts = %v, want 5", n)
	}

	// The month buttons.
	p.eval(`document.querySelector('.due-menu .cal-nav button[aria-label="次の月"]').click()`, nil)
	next := time.Date(now.AddDate(0, 0, 2).Year(), now.AddDate(0, 0, 2).Month()+1, 1, 0, 0, 0, 0, time.Local)
	p.until("next month", title+` === '`+monthText(next)+`'`)
	p.eval(`document.querySelector('.due-menu .cal-nav button[aria-label="前の月"]').click()`, nil)
	p.until("back", title+` === '`+monthText(now.AddDate(0, 0, 2))+`'`)
	p.esc()
	p.until("closed", `!document.querySelector('.due-menu')`)

	// The arrows cross into the next month: six weeks on from today.
	p.openMenu(one)
	p.until("today focused", `(document.activeElement.dataset || {}).day === '`+day(0)+`'`)
	for i := 0; i < 6; i++ {
		p.key(kb.ArrowDown)
	}
	p.until("month followed the arrows", title+` === '`+monthText(now.AddDate(0, 0, 42))+`'`)
	p.esc()
	p.until("closed again", `!document.querySelector('.due-menu')`)

	// A day pressed becomes the due date.
	p.openMenu(one)
	p.pickDay(day(4))
	a.untilTask(t, one, "due on the pressed day", func(k map[string]any) bool { return str(k, "due") == day(4) })

	// +1日 and +3日 count from today for a task with no date.
	p.openMenu(three)
	if got := p.str(`Array.from(document.querySelectorAll('.due-menu .dm-shift')).find(b => b.textContent.startsWith('+3日')).querySelector('.dm-hint').textContent`); got != now.AddDate(0, 0, 3).Format("1/2") {
		t.Errorf("+3日 hint = %q, want %q", got, now.AddDate(0, 0, 3).Format("1/2"))
	}
	p.pickItem("+3日")
	a.untilTask(t, three, "due +3 days", func(k map[string]any) bool { return str(k, "due") == day(3) })
	p.until("the list has the new date", chipOf(three)+`.dataset.due === '`+day(3)+`'`)
	p.openMenu(three)
	// The base is the due date when it is still ahead: +1日 from day(3) is day(4).
	p.pickItem("+1日")
	a.untilTask(t, three, "due +1 day from the date", func(k map[string]any) bool { return str(k, "due") == day(4) })
}
