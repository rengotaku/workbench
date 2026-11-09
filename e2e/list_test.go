//go:build e2e

package e2e

import (
	"reflect"
	"testing"
)

// Seeded tasks land in the right sections of the todo list.
func TestTodoListSections(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "queue-one")
	a.addTask(t, "queue-two")
	wait := a.addTask(t, "waiting-one")
	later := a.addTask(t, "someday-one")
	done := a.addTask(t, "done-one")
	a.post(t, "/api/tasks/"+wait+"/wait", map[string]any{"to": "相手"})
	a.post(t, "/api/tasks/"+later+"/someday", map[string]any{})
	a.post(t, "/api/tasks/"+done+"/done", map[string]any{})

	p := newPage(t, a, "/")
	p.until("rows drawn", `document.querySelectorAll('#queue .task').length === 2`)

	if got, want := p.texts(`#queue .txt`), []string{"queue-one", "queue-two"}; !reflect.DeepEqual(got, want) {
		t.Errorf("queue = %v, want %v", got, want)
	}
	for _, s := range []struct{ name, fold, list, text string }{
		{"待ち", "wait-fold", "waitlist", "waiting-one"},
		{"いつか", "someday-fold", "somedaylist", "someday-one"},
		{"今週完了", "done-fold", "donelist", "done-one"},
	} {
		// The section is folded until its head is clicked.
		p.until(s.name+" section", `!document.getElementById('`+s.fold+`').closest('section').hidden`)
		if hidden := p.str(`String(document.getElementById('` + s.list + `').hidden)`); hidden != "true" {
			t.Errorf("%s list should start folded", s.name)
		}
		p.click("#" + s.fold)
		p.until(s.name+" opened", `!document.getElementById('`+s.list+`').hidden`)
		if got, want := p.texts("#"+s.list+" .txt"), []string{s.text}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", s.name, got, want)
		}
	}
	// The other sections do not hold what belongs elsewhere.
	if got := p.texts(`#queue .txt`); len(got) != 2 {
		t.Errorf("queue grew to %v", got)
	}
}
