//go:build e2e

package e2e

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// dragRow plays a drag of one row onto another through the page's own drag
// handlers: start, over the lower half of dst, then drop and end. Without drop
// the drag is cancelled, as Escape would.
func (p *page) dragRow(src, dst string, drop bool) {
	p.t.Helper()
	p.eval(fmt.Sprintf(`(() => {
		const dt = new DataTransfer();
		const s = document.querySelector(%q), d = document.querySelector(%q);
		const fire = (el, type, extra) => el.dispatchEvent(new DragEvent(type, Object.assign({bubbles: true, cancelable: true, dataTransfer: dt}, extra)));
		fire(s, 'dragstart');
		const r = d.getBoundingClientRect();
		fire(d, 'dragover', {clientY: r.top + r.height * 0.9});
		if (%v) fire(s, 'drop');
		fire(s, 'dragend');
		return true;
	})()`, src, dst, drop), nil)
}

// addTaskDue seeds a task with a due date and returns its id.
func (a *app) addTaskDue(t *testing.T, name, due string) string {
	t.Helper()
	out := a.post(t, "/api/tasks", map[string]any{"name": name, "due": due})
	task, ok := out["task"].(map[string]any)
	if !ok {
		t.Fatalf("no task in the answer: %v", out)
	}
	return fmt.Sprint(task["id"])
}

// What the list's own controls do: scope, folds, grouping, the due menu, drag
// and drop (cancelled and dropped), the checkbox, the trash, resuming a waiting
// task and a long-run task's add-child button.
func TestTodoListControls(t *testing.T) {
	a := startApp(t)
	one := a.addTaskDue(t, "one", time.Now().AddDate(0, 0, 2).Format("2006-01-02"))
	a.addTask(t, "two")
	a.addTask(t, "three")
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "x"})
	long := a.addTask(t, "longone")
	a.post(t, "/api/tasks/"+long+"/long", map[string]any{"on": true})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 3`)

	// Scope buttons: one is pressed; 指定 shows its date field.
	p.click(`[data-scope="2weeks"]`)
	p.until("2weeks pressed", `document.querySelector('[data-scope="2weeks"]').getAttribute('aria-pressed') === 'true' && document.querySelector('[data-scope="week"]').getAttribute('aria-pressed') === 'false'`)
	p.click(`[data-scope="custom"]`)
	p.until("date field shown", `!document.getElementById('scope-until-wrap').hidden`)
	p.click(`[data-scope="week"]`)
	p.until("date field hidden", `document.getElementById('scope-until-wrap').hidden`)

	// A fold opens, and grouping by topic toggles.
	p.click(`#long-fold`)
	p.until("long open", `!document.getElementById('long-list').hidden && document.getElementById('long-fold').getAttribute('aria-expanded') === 'true' && document.querySelector('#long-fold .caret').textContent === '▼'`)
	p.click(`#group-toggle`)
	p.until("grouped", `document.getElementById('group-toggle').getAttribute('aria-pressed') === 'true' && !document.getElementById('group-note').hidden && document.querySelectorAll('#queue .topic-head').length >= 1`)
	p.click(`#group-toggle`)
	p.until("ungrouped", `document.querySelectorAll('#queue .topic-head').length === 0`)

	// The due chip opens a menu; Escape closes it.
	chip := `#queue .task:nth-child(1) .due-edit`
	p.click(chip)
	p.until("menu open", `document.querySelector('.due-menu') && document.querySelector('`+chip+`').getAttribute('aria-expanded') === 'true'`)
	p.key("\u001b")
	p.until("menu closed", `!document.querySelector('.due-menu') && document.querySelector('`+chip+`').getAttribute('aria-expanded') === 'false'`)

	// Drag: a cancelled drag puts the rows back; a drop moves the row and the
	// open row stays open.
	p.eval(`document.querySelectorAll('#queue .task')[2].click()`, nil)
	p.until("third open", `document.querySelectorAll('.task.row-open').length === 1`)
	var names []string
	p.dragRow(`#queue .task[data-id="`+one+`"]`, `#queue .task:nth-child(3)`, false)
	p.eval(queueNames, &names)
	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(names, want) {
		t.Errorf("after a cancelled drag = %v, want %v", names, want)
	}
	p.dragRow(`#queue .task[data-id="`+one+`"]`, `#queue .task:nth-child(3)`, true)
	p.until("reordered", queueNames+`.join() === 'two,three,one'`)
	if n := p.num(`document.querySelectorAll('.task.row-open').length`); n != 1 {
		t.Errorf("open rows after the drop = %v, want 1", n)
	}
	// The drag released the poll: a reload still reads.
	a.addTask(t, "four")
	p.reload()
	p.until("four drawn", queueHas(4, "four"))

	// The checkbox completes a task; the trash removes one.
	p.click(`#queue .task:nth-child(1) .check`)
	p.until("moved to done", `document.querySelectorAll('#queue .task').length === 3`)
	p.click(`#done-fold`)
	p.until("done list", `document.querySelectorAll('#donelist .task').length === 1`)
	p.run(chromedp.Click(`#queue .task:nth-child(1) .del`, chromedp.ByQuery))
	p.until("deleted", `document.querySelectorAll('#queue .task').length === 2`)

	// Resuming a waiting task tints its row for a moment (10 seconds), then the tint goes.
	p.click(`#wait-fold`)
	p.until("waiting row", `document.querySelectorAll('#waitlist .task').length === 1`)
	p.eval(`document.querySelector('#waitlist .task').click()`, nil)
	p.until("waiting open", `document.querySelectorAll('#waitlist .task.row-open').length === 1`)
	p.eval(`Array.from(document.querySelectorAll('#waitlist .panel button')).find(b => b.textContent === '着手順へ戻す').click()`, nil)
	p.until("landed", `document.querySelectorAll('.task.landed').length === 1`)
	p.untilWithin(12*time.Second, "landed gone", `document.querySelectorAll('.task.landed').length === 0`)

	// A long-run task with no next step offers 子を追加, which opens the add
	// dialog without opening the row.
	p.click(`#long-list .ln-add`)
	p.until("dialog", `document.querySelector('dialog[open]')`)
	p.esc()
	p.until("dialog closed", `!document.querySelector('dialog[open]')`)
	if n := p.num(`document.querySelectorAll('#long-list .task.row-open').length`); n != 0 {
		t.Errorf("子を追加 opened the row (%v open)", n)
	}
}
