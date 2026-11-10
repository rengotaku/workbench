//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// bool evaluates a JS expression and returns whether it is truthy.
func (p *page) bool(js string) bool {
	p.t.Helper()
	var ok bool
	p.eval("!!("+js+")", &ok)
	return ok
}

// get reads a JSON answer from the API.
func (a *app) get(t *testing.T, path string) map[string]any {
	t.Helper()
	res, err := http.Get(a.base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("GET %s: bad JSON: %v", path, err)
	}
	return out
}

// pasteLink pastes a copied link (HTML with an anchor, plus its plain text)
// into the add screen's name, as the clipboard would.
func (p *page) pasteLink(url, text string) {
	p.t.Helper()
	p.eval(fmt.Sprintf(`(() => {
		const node = document.getElementById('ap-name');
		node.focus();
		const dt = new DataTransfer();
		dt.setData('text/html', '<a href="%s">%s</a>');
		dt.setData('text/plain', %q);
		node.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
		return true;
	})()`, url, text, text), nil)
}

// addRowNumbers reads the numbers the rows of the add screen show.
const addRowNumbers = `Array.from(document.querySelectorAll('#ap-links .link-row .ln')).map(e => e.textContent).join(',')`

// What is typed on the add page survives a draw: the name, the detail, a row
// of related URLs and the caret are the same elements with the same text when a
// chip is pressed, a link is pasted (a row is added) or a row is removed.
func TestAddScreenKeepsInputAcrossRedraws(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/add")
	p.until("add screen", `document.getElementById('ap-name')`)

	p.run(chromedp.SendKeys(`#ap-name`, "abcdef", chromedp.ByQuery))
	p.run(chromedp.SendKeys(`#ap-detail`, "メモ", chromedp.ByQuery))
	p.click(`#ap-links .link-add`)
	p.run(chromedp.SendKeys(`#ap-links .link-input`, "https://example.com/a one", chromedp.ByQuery))
	p.eval(`document.getElementById('ap-name').focus()`, nil)
	p.until("row shown as a link", `document.querySelector('#ap-links .link-view') && !document.querySelector('#ap-links .link-view').hidden`)
	// Mark the elements; a draw that replaced them would lose the mark.
	p.eval(`(() => {
		document.getElementById('ap-name').__kept = 1;
		document.getElementById('ap-detail').__kept = 1;
		document.querySelector('#ap-links .link-input').__kept = 1;
		const name = document.getElementById('ap-name');
		name.focus();
		name.setSelectionRange(3, 3);
		return true;
	})()`, nil)

	// A link pasted at the caret: the text goes in followed by [2], the URL
	// becomes the second row, and the name keeps the focus.
	p.pasteLink("https://example.com/b", "TICKET-2")
	p.until("second row", `document.querySelectorAll('#ap-links .link-row').length === 2`)
	if got := p.str(`document.getElementById('ap-name').value`); got != "abcTICKET-2 [2]def" {
		t.Errorf("name after the paste = %q", got)
	}
	if got := p.str(addRowNumbers); got != "[1],[2]" {
		t.Errorf("row numbers = %q, want [1],[2]", got)
	}
	// A chip press draws again too.
	p.click(`#ap-chips .ap-chip:nth-child(3)`)
	p.until("明日 pressed", `document.querySelectorAll('#ap-chips .ap-chip')[2].getAttribute('aria-pressed') === 'true'`)

	for _, c := range []struct{ what, js string }{
		{"name element and text", `document.getElementById('ap-name').__kept === 1 && document.getElementById('ap-name').value.startsWith('abc')`},
		{"detail element and text", `document.getElementById('ap-detail').__kept === 1 && document.getElementById('ap-detail').value === 'メモ'`},
		{"first row element and text", `document.querySelector('#ap-links .link-input').__kept === 1 && document.querySelector('#ap-links .link-input').value === 'https://example.com/a one'`},
		{"name has the focus (the chip gave it back)", `document.activeElement === document.getElementById('ap-name')`},
	} {
		if !p.bool(c.js) {
			t.Errorf("after the draws: %s lost", c.what)
		}
	}
}

// Removing a row renumbers the rest, and a row the name still points at (or
// one before it) cannot be removed.
func TestAddScreenRenumbersLinkRows(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/add")
	p.until("add screen", `document.getElementById('ap-name')`)
	p.pasteLink("https://example.com/1", "one")
	p.pasteLink("https://example.com/2", "two")
	p.pasteLink("https://example.com/3", "three")
	p.until("three rows", `document.querySelectorAll('#ap-links .link-row').length === 3`)
	if got := p.str(`document.getElementById('ap-name').value`); got != "one [1]two [2]three [3]" {
		t.Fatalf("name = %q, want each paste to carry its own number", got)
	}

	// The name points at [2] and [3]: removing row 2 is refused, row 1 too.
	p.eval(`document.querySelectorAll('#ap-links .link-remove')[1].click()`, nil)
	p.until("refusal told", `document.getElementById('ap-toast').textContent.includes('[2] は外せません') && document.getElementById('ap-toast').classList.contains('is-error')`)
	if n := p.num(`document.querySelectorAll('#ap-links .link-row').length`); n != 3 {
		t.Fatalf("rows after a refused removal = %v, want 3", n)
	}

	// Once the name stops pointing at [3], row 3 can go; the row before it keeps its number.
	p.eval(`(() => { document.getElementById('ap-name').value = 'one [1]two [2]'; return true; })()`, nil)
	p.eval(`document.querySelectorAll('#ap-links .link-remove')[2].click()`, nil)
	p.until("two rows", `document.querySelectorAll('#ap-links .link-row').length === 2`)
	if got := p.str(addRowNumbers); got != "[1],[2]" {
		t.Errorf("row numbers = %q, want [1],[2]", got)
	}

	// An empty name and no pointers: row 1 goes, the old row 2 becomes [1].
	p.eval(`(() => { document.getElementById('ap-name').value = 'x'; return true; })()`, nil)
	p.eval(`document.querySelectorAll('#ap-links .link-remove')[0].click()`, nil)
	p.until("one row", `document.querySelectorAll('#ap-links .link-row').length === 1`)
	if got := p.str(`document.querySelector('#ap-links .link-view').href`); got != "https://example.com/2" {
		t.Errorf("remaining row = %q, want the second URL", got)
	}
	if got := p.str(addRowNumbers); got != "[1]" {
		t.Errorf("row numbers = %q, want [1]", got)
	}
	if got := p.str(`document.querySelector('#ap-links .link-remove').getAttribute('aria-label')`); got != "関連 URL [1] を外す" {
		t.Errorf("remove label = %q", got)
	}
}

// Submitting sends the name, the due date and the related URLs as typed, then
// clears the form and lists what went in; 元に戻す takes it back.
func TestAddScreenSendsWhatWasTyped(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/add")
	p.until("add screen", `document.getElementById('ap-name')`)

	p.run(chromedp.SendKeys(`#ap-name`, "see ", chromedp.ByQuery))
	p.pasteLink("https://example.com/t", "TICKET-9")
	p.run(chromedp.SendKeys(`#ap-detail`, "details", chromedp.ByQuery))
	p.click(`#ap-chips .ap-chip:nth-child(2)`) // 今日
	today := p.str(`document.querySelectorAll('#ap-chips .ap-chip')[1].getAttribute('aria-pressed') === 'true' ? document.getElementById('ap-resolved').textContent.replace('→ ', '') : ''`)
	if today == "" {
		t.Fatal("the 今日 chip did not resolve to a date")
	}
	p.click(`#ap-submit`)
	p.until("listed", `document.querySelector('#ap-recent .ap-recent-name')?.textContent === 'see TICKET-9 [1]'`)
	if got := p.str(`document.querySelector('#ap-recent .ap-recent-due').textContent`); got != today {
		t.Errorf("listed due = %q, want %q", got, today)
	}
	// The form is clear for the next task and the caret is back in the name.
	for _, c := range []struct{ what, js string }{
		{"name cleared", `document.getElementById('ap-name').value === ''`},
		{"detail cleared", `document.getElementById('ap-detail').value === ''`},
		{"rows cleared", `document.querySelectorAll('#ap-links .link-row').length === 0`},
		{"due back to なし", `document.querySelector('#ap-chips .ap-chip').getAttribute('aria-pressed') === 'true' && document.getElementById('ap-resolved').textContent === ''`},
		{"caret back in the name", `document.activeElement === document.getElementById('ap-name') && !document.getElementById('ap-name').disabled`},
	} {
		p.until(c.what, c.js)
	}

	board := a.get(t, "/api/tasks?scope=all")
	queue, _ := board["queue"].([]any)
	if len(queue) != 1 {
		t.Fatalf("queue = %v, want the one task", queue)
	}
	task, _ := queue[0].(map[string]any)
	if got := str(task, "name"); got != "see TICKET-9 [1]" {
		t.Errorf("name = %q", got)
	}
	if got := str(task, "due"); got != today {
		t.Errorf("due = %q, want %q", got, today)
	}
	if got := str(task, "detail"); got != "details" {
		t.Errorf("detail = %q", got)
	}
	links, _ := task["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v, want one", links)
	}
	l, _ := links[0].(map[string]any)
	if str(l, "url") != "https://example.com/t" || str(l, "label") != "TICKET-9" {
		t.Errorf("link = %v", l)
	}

	p.eval(`document.querySelector('#ap-toast button').click()`, nil)
	p.until("undone", `document.querySelector('#ap-recent li').classList.contains('is-undone') && document.getElementById('ap-toast').textContent.startsWith('取り消しました: see TICKET-9 [1]')`)
	if left := a.task(t, str(task, "id")); left != nil {
		t.Errorf("the task is still on the board after 元に戻す: %v", left)
	}
}

// The add dialog lends the screen: what was half typed there (name, due,
// detail, a link row) is still there after closing and opening it again.
func TestAddDialogKeepsADraftAcrossClosing(t *testing.T) {
	a := startApp(t)
	a.addTask(t, "existing")
	p := newPage(t, a, "/")
	p.until("row", `document.querySelectorAll('#queue .task').length === 1`)

	open := func() {
		p.click(`#add-trigger`)
		p.until("dialog open", `document.querySelector('dialog[open] #ap-name')`)
	}
	open()
	p.run(chromedp.SendKeys(`#ap-name`, "書きかけ", chromedp.ByQuery))
	p.run(chromedp.SendKeys(`#ap-detail`, "下書き", chromedp.ByQuery))
	p.pasteLink("https://example.com/d", "DRAFT-1")
	p.click(`dialog[open] #ap-chips .ap-chip:nth-child(3)`)
	p.until("link row in the dialog", `document.querySelectorAll('dialog[open] .link-row').length === 1`)
	p.esc()
	p.until("closed", `document.querySelectorAll('dialog[open]').length === 0 && !document.querySelector('dialog')`)
	if !p.bool(`document.querySelector('[data-view="todo-add"]').hidden`) {
		t.Fatal("the add screen was not given back")
	}

	open()
	for _, c := range []struct{ what, js string }{
		{"name", `document.getElementById('ap-name').value === '書きかけDRAFT-1 [1]'`},
		{"detail", `document.getElementById('ap-detail').value === '下書き'`},
		{"link row", `document.querySelectorAll('dialog[open] .link-row').length === 1 && document.querySelector('dialog[open] .link-view').href === 'https://example.com/d'`},
		{"due chip", `document.querySelectorAll('dialog[open] #ap-chips .ap-chip')[2].getAttribute('aria-pressed') === 'true'`},
		{"caret in the name", `document.activeElement === document.getElementById('ap-name')`},
	} {
		if !p.bool(c.js) {
			t.Errorf("after reopening: %s lost", c.what)
		}
	}
}

// 子を追加 opens the screen with ?parent=: the heading says whose child, and the
// task is sent with its parent.
func TestAddScreenParentFromTheQuery(t *testing.T) {
	a := startApp(t)
	parent := a.addTask(t, "parent")
	p := newPage(t, a, "/add?parent="+parent)
	p.until("the parent line", `!document.getElementById('ap-parent').hidden`)
	if got := p.str(`document.getElementById('ap-parent').textContent`); got != parent+" の子として追加する" {
		t.Errorf("parent line = %q", got)
	}
	p.run(chromedp.SendKeys(`#ap-name`, "child", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("listed", `document.querySelector('#ap-recent .ap-recent-name')?.textContent === 'child'`)
	var childParent string
	for _, section := range []string{"queue", "focus"} {
		board := a.get(t, "/api/tasks?scope=all")
		list, _ := board[section].([]any)
		for _, raw := range list {
			if m, ok := raw.(map[string]any); ok && str(m, "name") == "child" {
				childParent = str(m, "parent")
			}
		}
	}
	if childParent != parent {
		t.Errorf("child's parent = %q, want %q", childParent, parent)
	}
}

// The open row's related URLs: adding two saves each, removing the first
// saves the list without it, and the rows are renumbered.
func TestTodoDetailLinksAddRemoveAreSaved(t *testing.T) {
	a := startApp(t)
	_, second, p := openSecond(t, a)
	count := func(n int) func(map[string]any) bool {
		return func(m map[string]any) bool { links, _ := m["links"].([]any); return len(links) == n }
	}
	urls := func() string {
		links, _ := a.task(t, second)["links"].([]any)
		var out []string
		for _, raw := range links {
			l, _ := raw.(map[string]any)
			out = append(out, str(l, "url"))
		}
		return strings.Join(out, ",")
	}

	p.click(`.task.row-open .link-add`)
	p.run(chromedp.SendKeys(`.task.row-open .link-row:last-child .link-input`, "https://example.com/a first", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, second, "the first URL", count(1))
	p.click(`.task.row-open .link-add`)
	p.run(chromedp.SendKeys(`.task.row-open .link-row:last-child .link-input`, "https://example.com/b second", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, second, "two URLs", count(2))
	if got := urls(); got != "https://example.com/a,https://example.com/b" {
		t.Errorf("saved URLs = %q", got)
	}

	p.eval(`document.querySelectorAll('.task.row-open .link-remove')[0].click()`, nil)
	a.untilTask(t, second, "one URL", count(1))
	if got := urls(); got != "https://example.com/b" {
		t.Errorf("saved URLs after the removal = %q, want only the second", got)
	}
	p.until("renumbered", `document.querySelectorAll('.task.row-open .link-row').length === 1 && document.querySelector('.task.row-open .link-row .ln').textContent === '[1]'`)
	// The page and the server agree after a reload.
	p.reload()
	p.until("the saved row", `document.querySelectorAll('.task.row-open .link-row').length === 1 && document.querySelector('.task.row-open .link-view')?.href === 'https://example.com/b'`)
}
