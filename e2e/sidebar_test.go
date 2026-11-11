//go:build e2e

package e2e

import (
	"fmt"
	"testing"
)

// The todo screen has its own sidebar: it counts the sections, opens a section
// and scrolls to it when pressed, and the header nav is hidden on the todo
// screens and shown on the note ones.
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
	if got := p.str(`getComputedStyle(document.querySelector('.site-head')).display`); got != "none" {
		t.Errorf("header nav on the todo screen: display = %q, want none", got)
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
	if got := p.str(`localStorage.getItem('todo-order.someday-open')`); got != "1" {
		t.Errorf("saved open state = %q, want 1", got)
	}

	// The links go to the note screens in the page, where the header nav shows.
	p.nav("/notes")
	p.until("note shown", `document.querySelector('[data-view="note-add"]') && !document.querySelector('[data-view="note-add"]').hidden`)
	if got := p.str(`getComputedStyle(document.querySelector('.site-head')).display`); got == "none" {
		t.Errorf("header nav on the note screen is hidden, want shown")
	}
}
