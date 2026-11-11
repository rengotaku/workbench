//go:build e2e

package e2e

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The detail panel keeps the same task after an operation, so its button
// opens again and does the opposite: 完了にする, then 完了を取り消す. Completing
// moves the task to 今週完了, and the panel stays open for it.
func TestTodoSideActReopens(t *testing.T) {
	a := startApp(t)
	id := a.addTask(t, "toggle me")
	p := newPage(t, a, "/")
	p.until("row", `!!document.querySelector('.task[data-id="`+id+`"]')`)
	p.openRow(id)

	p.eval(`document.querySelector('.d-complete').click(), true`, nil)
	p.until("undo label, enabled", `(() => { const b = document.querySelector('.d-complete'); return b && b.textContent === '完了を取り消す' && !b.disabled; })()`)
	p.eval(`document.querySelector('.d-complete').click(), true`, nil)
	p.until("back to 完了にする, enabled", `(() => { const b = document.querySelector('.d-complete'); return b && b.textContent === '完了にする' && !b.disabled; })()`)
}

// The panel has one status input, and none while no row is open.
func TestTodoSideOneStatusInput(t *testing.T) {
	a := startApp(t)
	id := a.addTask(t, "one input")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)
	if n := p.num(`document.querySelectorAll('.status-input').length`); n != 0 {
		t.Errorf("status inputs before a row is open = %v, want 0", n)
	}
	p.openRow(id)
	if n := p.num(`document.querySelectorAll('.status-input').length`); n != 1 {
		t.Errorf("status inputs with a row open = %v, want 1", n)
	}
}

// At 1440px wide the table fits beside the sidebar; it does not
// scroll sideways.
func TestTodoTableFitsAt1440(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "fits")
	p := newPage(t, a, "/")
	for _, w := range []int64{1440, 1600} {
		p.run(chromedp.EmulateViewport(w, 900))
		p.until("table drawn", `!!document.querySelector('.tbl-scroll')`)
		if over := p.num(`(() => { const b = document.querySelector('.tbl-scroll'); return b.scrollWidth - b.clientWidth; })()`); over > 1 {
			t.Errorf("at %dpx the table scrolls sideways by %v px", w, over)
		}
	}
}
