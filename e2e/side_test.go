//go:build e2e

package e2e

import (
	"testing"
)

// The box beside the list lays the task out as fields. Its 期限 button opens
// the due menu; for a waiting task the button is shut. The clicks are made from
// the page's script: the table scrolls sideways at this width, and a real click
// that scrolls would close the menu it opens.
func TestTodoSideFields(t *testing.T) {
	a := startApp(t)
	open := a.addTask(t, "open one")
	waiter := a.addTask(t, "waiter")
	a.post(t, "/api/tasks/"+waiter+"/wait", map[string]any{"to": "高橋"})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)

	show := func(id string) {
		p.eval(`document.querySelector('.task[data-id="`+id+`"]').dispatchEvent(new MouseEvent('mouseenter'))`, nil)
		p.until("box shows "+id, `document.querySelector('#detail .d-id') && document.querySelector('#detail .d-id').textContent === '`+id+`'`)
	}

	// An open-ended queue task: the name large, the fields named, the due
	// button warns and opens the menu.
	show(open)
	if got := p.str(`Array.from(document.querySelectorAll('#detail .d-fname')).map(n => n.textContent).join(',')`); got != "状態,期限,話題,フォーカス,止まって" {
		t.Errorf("fields = %q", got)
	}
	if got := p.num(`parseFloat(getComputedStyle(document.querySelector('#detail .d-name')).fontSize)`); got < 19 {
		t.Errorf("name font size = %v, want about 20", got)
	}
	if got := p.str(`document.querySelector('#detail .d-due').textContent.trim()`); got != "⚠ 期限なし" {
		t.Errorf("due button = %q, want the warning", got)
	}
	p.eval(`document.querySelector('#detail .d-due').click()`, nil)
	p.until("due menu open", `!!document.querySelector('.due-menu') && document.querySelector('#detail .d-due').getAttribute('aria-expanded') === 'true'`)
	// Another row under the pointer must not swap the box while the menu is open.
	p.eval(`document.querySelector('.task[data-id="`+open+`"]').dispatchEvent(new MouseEvent('mouseenter'))`, nil)
	p.esc()
	p.until("due menu closed", `!document.querySelector('.due-menu')`)

	// A waiting task: the button cannot be pressed, and the fields say who and when.
	p.click(`#wait-fold`)
	p.until("waiting row", `document.querySelectorAll('#waitlist .task').length === 1`)
	show(waiter)
	if n := p.num(`document.querySelectorAll('#detail .d-due:disabled').length`); n != 1 {
		t.Errorf("shut due buttons = %v, want 1", n)
	}
	if got := p.str(`document.querySelector('#detail .d-due').textContent.trim()`); got != "待ちの間は使わない（戻すと元の期限）" {
		t.Errorf("waiting due button = %q", got)
	}
	if got := p.str(`Array.from(document.querySelectorAll('#detail .d-fname')).map(n => n.textContent).join(',')`); got != "状態,期限,話題,止まって,渡した相手,渡した日" {
		t.Errorf("waiting fields = %q", got)
	}
	if n := p.num(`document.querySelectorAll('.due-menu').length`); n != 0 {
		t.Errorf("a due menu is open for a waiting task")
	}
	// The moves for a waiting task sit under the fields.
	if got := p.str(`Array.from(document.querySelectorAll('#detail .d-moves button')).map(b => b.textContent).join(',')`); got != "着手順へ戻す,いつかにする" {
		t.Errorf("moves = %q", got)
	}
}
