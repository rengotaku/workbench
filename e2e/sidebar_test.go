//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

// The todo screen has its own sidebar: it counts the sections, opens a section
// and scrolls to it when pressed, and the header band shows on the todo screen.
func TestTodoSidebarOpensAndScrollsToASection(t *testing.T) {
	a := startApp(t)
	for i := 0; i < 30; i++ {
		a.addTask(t, fmt.Sprintf("filler %d", i))
	}
	later := a.addTask(t, "someday-one")
	a.post(t, "/api/tasks/"+later+"/someday", map[string]any{})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 30`)

	// The counts are the sections' own counts, and a stale scope saved by the
	// old buttons is not read.
	p.until("queue count", `document.querySelector('[data-section="queue"] .side-n').textContent === '30'`)
	p.until("someday count", `document.querySelector('[data-section="someday"] .side-n').textContent === '1'`)
	if got := p.str(`document.getElementById('someday-count').textContent`); got != "1" {
		t.Errorf("someday heading count = %q, want 1", got)
	}
	if got := p.str(`getComputedStyle(document.querySelector('.site-head')).display`); got == "none" {
		t.Errorf("header band on the todo screen is hidden, want shown")
	}
	if n := p.num(`document.querySelectorAll('.side-links').length`); n != 0 {
		t.Errorf("sidebar links = %v, want none (the band has them)", n)
	}
	// The band sits right on the body: no gap between them.
	if got := p.num(`document.querySelector('.panes').getBoundingClientRect().top - document.querySelector('.site-head').getBoundingClientRect().bottom`); got != 0 {
		t.Errorf("gap between the band and the body = %v, want 0", got)
	}

	// The someday section is closed and below the fold; its sidebar button
	// opens it, remembers that, and scrolls to it.
	p.until("someday closed", `document.getElementById('somedaylist').hidden`)
	if y := p.num(`window.scrollY`); y != 0 {
		t.Fatalf("scrollY before = %v, want 0", y)
	}
	p.click(`[data-section="someday"]`)
	p.until("someday open", `!document.getElementById('somedaylist').hidden && document.getElementById('someday-fold').getAttribute('aria-expanded') === 'true'`)
	p.until("scrolled to it", `window.scrollY > 100 && document.getElementById('someday-sec').getBoundingClientRect().top < window.innerHeight`)
	p.until("keyboard moved into it", `document.getElementById('someday-sec').contains(document.activeElement)`)
	if got := p.str(`localStorage.getItem('todo-order.someday-open')`); got != "1" {
		t.Errorf("saved open state = %q, want 1", got)
	}

	// The band's links go to the note screens in the page.
	p.nav("/notes")
	p.until("note shown", `document.querySelector('[data-view="note-add"]') && !document.querySelector('[data-view="note-add"]').hidden`)
	if got := p.str(`getComputedStyle(document.querySelector('.site-head')).display`); got == "none" {
		t.Errorf("header nav on the note screen is hidden, want shown")
	}
}

// A topic pressed in the sidebar narrows the list to that topic's tasks; the
// counts follow, the topic counts do not, and pressing again clears it.
func TestTodoSidebarTopicFilter(t *testing.T) {
	a := startApp(t)
	x := a.addTask(t, "alpha one")
	y := a.addTask(t, "alpha two")
	z := a.addTask(t, "beta one")
	a.addTask(t, "no topic")
	a.patch(t, x, map[string]any{"topic": "A"})
	a.patch(t, y, map[string]any{"topic": "A"})
	a.patch(t, z, map[string]any{"topic": "B"})
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 4`)
	p.until("topics listed", `document.querySelectorAll('#side-topics [data-topic]').length === 2`)

	p.eval(`document.querySelector('#side-topics [data-topic="A"]').click()`, nil)
	p.until("narrowed", `document.querySelectorAll('#queue .task').length === 2`)
	if got := p.str(`document.querySelector('[data-section="queue"] .side-n').textContent`); got != "2" {
		t.Errorf("queue count in the sidebar = %q, want 2", got)
	}
	if got := p.str(`document.getElementById('queue-count').textContent`); got != "2" {
		t.Errorf("queue heading count = %q, want 2", got)
	}
	if got := p.str(`document.querySelector('#side-topics [data-topic="B"] .side-n').textContent`); got != "1" {
		t.Errorf("topic B count = %q, want 1 (topic counts are not narrowed)", got)
	}
	if got := p.str(`document.querySelector('#side-topics [data-topic="A"]').getAttribute('aria-pressed')`); got != "true" {
		t.Errorf("aria-pressed = %q, want true", got)
	}
	if got := p.str(`document.getElementById('topic-filter').textContent`); got != "話題: A ×" {
		t.Errorf("filter label = %q", got)
	}
	// While it is on, the queue cannot be reordered, and it says so.
	if n := p.num(`document.querySelectorAll('#queue .task[draggable="true"]').length`); n != 0 {
		t.Errorf("draggable rows while filtered = %v, want 0", n)
	}
	if got := p.str(`(e => e.hidden ? '' : e.textContent)(document.getElementById('group-note'))`); got != "絞っている間は並べ替えできない" {
		t.Errorf("note = %q", got)
	}
	// Together with 話題で分ける: the narrowed rows are split under their topic.
	p.eval(`document.getElementById('group-toggle').click()`, nil)
	p.until("grouped", `document.querySelectorAll('#queue .topic-head').length === 1 && document.querySelectorAll('#queue .task').length === 2`)
	p.eval(`document.getElementById('group-toggle').click()`, nil)
	p.until("ungrouped", `document.querySelectorAll('#queue .topic-head').length === 0`)

	// The mark in the heading clears it.
	p.eval(`document.getElementById('topic-filter').click()`, nil)
	p.until("all back", `document.querySelectorAll('#queue .task').length === 4 && document.getElementById('topic-filter').hidden`)
	p.eval(`document.querySelector('#side-topics [data-topic="B"]').click()`, nil)
	p.until("narrowed to B", `document.querySelectorAll('#queue .task').length === 1`)
	p.eval(`document.querySelector('#side-topics [data-topic="B"]').click()`, nil)
	p.until("pressing again clears", `document.querySelectorAll('#queue .task').length === 4`)

	// It is not kept: a reload clears it.
	p.eval(`document.querySelector('#side-topics [data-topic="A"]').click()`, nil)
	p.until("narrowed again", `document.querySelectorAll('#queue .task').length === 2`)
	p.run(chromedp.Reload())
	p.until("a reload shows all", `document.querySelectorAll('#queue .task').length === 4`)
}

// The sidebar folds: the list takes its room, the button to bring it back stays
// in the list's header, and the state is kept across a reload.
func TestTodoSidebarFolds(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "one")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 1`)

	listLeft := `document.querySelector('.app').getBoundingClientRect().left`
	before := p.num(listLeft)
	if got := p.str(`document.getElementById('side-fold').getAttribute('aria-expanded')`); got != "true" {
		t.Errorf("fold button aria-expanded = %q, want true", got)
	}
	p.eval(`document.getElementById('side-fold').click()`, nil)
	p.until("sidebar hidden", `getComputedStyle(document.querySelector('.side')).display === 'none'`)
	if after := p.num(listLeft); after >= before {
		t.Errorf("list left edge %v -> %v, want it to move left", before, after)
	}
	if got := p.str(`localStorage.getItem('todo-order.side-open')`); got != "0" {
		t.Errorf("saved state = %q, want 0", got)
	}
	if n := p.num(`document.querySelectorAll('#side-show:not([hidden])').length`); n != 1 {
		t.Errorf("show button on the screen = %v, want 1", n)
	}

	p.run(chromedp.Reload())
	p.until("still hidden after a reload", `document.querySelectorAll('#queue .task').length === 1 && getComputedStyle(document.querySelector('.side')).display === 'none'`)
	p.eval(`document.getElementById('side-show').click()`, nil)
	p.until("shown again", `getComputedStyle(document.querySelector('.side')).display !== 'none' && !!document.getElementById('side-fold')`)
	if got := p.str(`localStorage.getItem('todo-order.side-open')`); got != "1" {
		t.Errorf("saved state = %q, want 1", got)
	}

	// Stacked above the list at a narrow width, it folds too.
	p.run(chromedp.EmulateViewport(700, 900))
	p.eval(`document.getElementById('side-fold').click()`, nil)
	p.until("hidden when stacked", `getComputedStyle(document.querySelector('.side')).display === 'none'`)
}
