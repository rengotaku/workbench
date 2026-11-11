//go:build e2e

package e2e

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// The box beside the list keeps the same task after an operation, so its
// button opens again and does the opposite: 完了にする, then 完了を取り消す.
func TestTodoSideActReopens(t *testing.T) {
	a := startApp(t)
	id := a.addTask(t, "toggle me")
	p := newPage(t, a, "/")
	p.until("row", `!!document.querySelector('.task[data-id="`+id+`"]')`)
	p.until("box shows it", `document.querySelector('.d-id') && document.querySelector('.d-id').textContent === '`+id+`'`)

	p.eval(`document.querySelector('.d-complete').click(), true`, nil)
	p.until("undo label, enabled", `(() => { const b = document.querySelector('.d-complete'); return b && b.textContent === '完了を取り消す' && !b.disabled; })()`)
	p.eval(`document.querySelector('.d-complete').click(), true`, nil)
	p.until("back to 完了にする, enabled", `(() => { const b = document.querySelector('.d-complete'); return b && b.textContent === '完了にする' && !b.disabled; })()`)
}

// While a task's row is open, only the row has the status input; the box
// points to it, so one text cannot be appended twice from two inputs.
func TestTodoSideOneStatusInput(t *testing.T) {
	a := startApp(t)
	id := a.addTask(t, "one input")
	p := newPage(t, a, "/")
	p.until("box input", `!!document.getElementById('d-status-`+id+`')`)
	p.eval(`document.querySelector('.task[data-id="`+id+`"] .name').click(), true`, nil)
	p.until("row open, box points to it", `!document.getElementById('d-status-`+id+`') && !!document.querySelector('.d-status-here')`)
}

// At 1440px wide the table fits beside the sidebar and the box; it does not
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
