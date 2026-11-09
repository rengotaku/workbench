//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// task reads one task from the API (whichever section it is in).
func (a *app) task(t *testing.T, id string) map[string]any {
	t.Helper()
	res, err := http.Get(a.base + "/api/tasks?scope=all")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var board map[string]any
	if err := json.NewDecoder(res.Body).Decode(&board); err != nil {
		t.Fatalf("GET /api/tasks: bad JSON: %v", err)
	}
	for _, section := range []string{"queue", "focus", "long", "waiting", "someday", "done"} {
		list, _ := board[section].([]any)
		for _, raw := range list {
			if task, ok := raw.(map[string]any); ok && fmt.Sprint(task["id"]) == id {
				return task
			}
		}
	}
	return nil
}

// untilTask waits until check holds for the task as the server has it.
func (a *app) untilTask(t *testing.T, id, what string, check func(task map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if task := a.task(t, id); task != nil && check(task) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s on %s: %v", what, id, a.task(t, id))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// openRow opens a row by its task id, as a click on it does.
func (p *page) openRow(id string) {
	p.t.Helper()
	p.until("row "+id, `document.querySelector('.task[data-id="`+id+`"]')`)
	p.eval(`document.querySelector('.task[data-id="`+id+`"]').click()`, nil)
	p.until("row "+id+" open", `document.querySelector('.task[data-id="`+id+`"].row-open .panel .panel-actions')`)
}

// clickButton presses the button with this text in the open row's panel.
func (p *page) clickButton(id, text string) {
	p.t.Helper()
	p.until("button "+text, `Array.from(document.querySelectorAll('.task[data-id="`+id+`"] .panel button')).some(b => b.textContent === '`+text+`')`)
	p.eval(`Array.from(document.querySelectorAll('.task[data-id="`+id+`"] .panel button')).find(b => b.textContent === '`+text+`').click()`, nil)
}

func str(task map[string]any, key string) string {
	v, ok := task[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// #115 for every field of the open row: a list reply that arrives while one of
// them is typed in leaves the same node, its text, the focus and the caret. The
// name, the detail, the topic, the status line, a related URL row and the
// recipient of 待ちにする.
func TestTodoDetailFieldsKeepTypingAcrossAReply(t *testing.T) {
	cases := []struct {
		name string
		prep func(p *page)
		sel  string
		prop string // the property holding the typed text
	}{
		{name: "name", sel: `.task.row-open .txt.live`, prop: "textContent"},
		{name: "detail", sel: `.task.row-open .live.detail`, prop: "textContent"},
		{name: "topic", sel: `.task.row-open .topic-input`, prop: "value"},
		{name: "status", sel: `.task.row-open .status-input`, prop: "value"},
		{
			name: "related URL",
			prep: func(p *page) {
				// Adding a row focuses it, which holds the poll: leave it first.
				p.click(`.task.row-open .link-add`)
				p.eval(`document.activeElement.blur()`, nil)
			},
			sel: `.task.row-open .link-input`, prop: "value",
		},
		{
			name: "wait-to",
			prep: func(p *page) {
				p.eval(`Array.from(document.querySelectorAll('.task.row-open .panel button')).find(b => b.textContent === '待ちにする').click()`, nil)
			},
			sel: `.task.row-open input[aria-label="渡した相手"]`, prop: "value",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := startApp(t)
			first, _, p := openSecond(t, a)
			if c.prep != nil {
				c.prep(p)
			}
			node := `document.querySelector('` + c.sel + `')`
			p.until("the field", node)
			p.eval(node+`.__mark = 'same'`, nil)

			// A change elsewhere moves the revision, so the next poll reads the
			// list; that read is held while the field is typed in.
			g := p.holdList(fetch.RequestStageRequest)
			a.addTask(t, "fourth")
			g.waitHeld(t, 10*time.Second)
			p.run(chromedp.SendKeys(c.sel, "typed", chromedp.ByQuery))
			// The row above the open one goes: the reply moves every row.
			a.done(t, first)
			g.release()
			p.until("list redrawn", queueHas(3, "fourth"))

			if got := p.str(node + `.__mark || ''`); got != "same" {
				t.Errorf("the %s field was replaced by the redraw", c.name)
			}
			if got := p.str(node + `.` + c.prop); !strings.Contains(got, "typed") {
				t.Errorf("%s text = %q, want the typed text kept", c.name, got)
			}
			if got := p.str(`String(document.activeElement === ` + node + `)`); got != "true" {
				t.Errorf("the %s field lost the focus", c.name)
			}
			if c.prop == "value" {
				if got := p.str(`String(` + node + `.selectionStart === ` + node + `.value.length)`); got != "true" {
					t.Errorf("the %s field lost the caret", c.name)
				}
			}
		})
	}
}

// Every action of the open row's detail does what it did before it was drawn
// by Preact: the same request, and the row shows the result.
func TestTodoDetailActions(t *testing.T) {
	a := startApp(t)
	main := a.addTask(t, "main task")
	waiter := a.addTask(t, "waiter task")
	longer := a.addTask(t, "longer task")
	focuser := a.addTask(t, "focuser task")
	other := a.addTask(t, "other task")
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 5`)
	row := func(id string) string { return `.task[data-id="` + id + `"]` }

	// Edit the name, the detail and the topic; add a status line.
	p.openRow(main)
	p.run(chromedp.SendKeys(row(main)+` .txt.live`, "XYZ", chromedp.ByQuery))
	p.eval(`document.activeElement.blur()`, nil)
	a.untilTask(t, main, "the name", func(m map[string]any) bool { return strings.Contains(str(m, "name"), "XYZ") })

	p.run(chromedp.SendKeys(row(main)+` .live.detail`, "some detail", chromedp.ByQuery))
	p.eval(`document.activeElement.blur()`, nil)
	a.untilTask(t, main, "the detail", func(m map[string]any) bool { return str(m, "detail") == "some detail" })

	p.run(chromedp.SendKeys(row(main)+` .topic-input`, "alpha", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, main, "the topic", func(m map[string]any) bool { return str(m, "topic") == "alpha" })

	p.run(chromedp.SendKeys(row(main)+` .status-input`, "going well", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, main, "the status line", func(m map[string]any) bool {
		anns, _ := m["annotations"].([]any)
		return len(anns) == 1
	})
	p.until("the status shown, the field empty", `document.querySelector('`+row(main)+` .d-ann-body')?.textContent === 'going well' && document.querySelector('`+row(main)+` .status-input').value === ''`)
	p.until("the trail link", `document.querySelector('`+row(main)+` .trail-link')?.textContent.startsWith('経緯 ')`)

	// A related URL is added by typing it and removed with 外す.
	p.click(row(main) + ` .link-add`)
	p.run(chromedp.SendKeys(row(main)+` .link-input`, "https://example.com/a label", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, main, "the related URL", func(m map[string]any) bool {
		links, _ := m["links"].([]any)
		return len(links) == 1
	})
	p.eval(`document.querySelector('`+row(main)+` .link-remove').click()`, nil)
	a.untilTask(t, main, "the URL removed", func(m map[string]any) bool {
		links, _ := m["links"].([]any)
		return len(links) == 0
	})

	// The copy button carries the task's address; 子を追加 opens the add
	// dialog; the parent picker ties the task to another.
	p.until("the copy button", `Array.from(document.querySelectorAll('`+row(main)+` .panel button')).some(b => b.textContent === 'URLをコピー' && b.title.includes('#`+main+`'))`)
	p.clickButton(main, "子を追加")
	p.until("add dialog", `document.querySelector('dialog[open]')`)
	p.esc()
	p.until("add dialog closed", `!document.querySelector('dialog[open]')`)
	p.eval(`(() => { const s = document.querySelector('`+row(main)+` .parent-pick select'); s.value = '`+other+`'; s.dispatchEvent(new Event('change', {bubbles: true})); return true; })()`, nil)
	a.untilTask(t, main, "the parent", func(m map[string]any) bool { return str(m, "parent") == other })

	// 待ちにする asks who holds the ball; 渡した sends it. From waiting the
	// task goes to いつか, and from there back to the queue.
	p.openRow(waiter)
	p.clickButton(waiter, "待ちにする")
	p.run(chromedp.SendKeys(row(waiter)+` input[aria-label="渡した相手"]`, "bob", chromedp.ByQuery))
	p.clickButton(waiter, "渡した")
	a.untilTask(t, waiter, "waiting", func(m map[string]any) bool { return str(m, "state") == "waiting" && str(m, "handed_to") == "bob" })
	p.click(`#wait-fold`)
	p.openRow(waiter)
	p.until("the hand-off note", `document.querySelector('`+row(waiter)+` .handoff-note')?.textContent.includes('bob')`)
	p.clickButton(waiter, "いつかにする")
	a.untilTask(t, waiter, "someday", func(m map[string]any) bool { return str(m, "state") == "someday" })
	p.click(`#someday-fold`)
	p.openRow(waiter)
	p.clickButton(waiter, "着手順へ戻す")
	a.untilTask(t, waiter, "back in the queue", func(m map[string]any) bool { return str(m, "state") == "open" })

	// 長期にする and 長期を外す.
	p.openRow(longer)
	p.clickButton(longer, "長期にする")
	a.untilTask(t, longer, "long", func(m map[string]any) bool { return str(m, "state") == "long" })
	p.click(`#long-fold`)
	p.openRow(longer)
	p.clickButton(longer, "長期を外す")
	a.untilTask(t, longer, "out of the long band", func(m map[string]any) bool { return str(m, "state") == "open" })

	// フォーカス.
	p.openRow(focuser)
	p.clickButton(focuser, "フォーカス")
	a.untilTask(t, focuser, "focused", func(m map[string]any) bool { return m["focused"] == true })
}

// A move above tasks that are due soon is asked about: the open row offers 理由を書く,
// which turns into a field in the same place; Enter records the reason, and the
// offer goes away once nothing is left unexplained.
func TestTodoDetailReasonEditor(t *testing.T) {
	a := startApp(t)
	// The third row has the nearest due date and goes above the other two,
	// which is what the server asks a reason for.
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	a.addTaskDue(t, "one", day(3))
	a.addTaskDue(t, "two", day(4))
	third := a.addTaskDue(t, "three", day(1))
	p := newPage(t, a, "/")
	p.until("rows", `document.querySelectorAll('#queue .task').length === 3`)

	// Drop the third row above the first.
	p.eval(`(() => {
		const dt = new DataTransfer();
		const s = document.querySelector('#queue .task[data-id="`+third+`"]'), d = document.querySelector('#queue .task:nth-child(1)');
		const fire = (el, type, extra) => el.dispatchEvent(new DragEvent(type, Object.assign({bubbles: true, cancelable: true, dataTransfer: dt}, extra)));
		fire(s, 'dragstart');
		const r = d.getBoundingClientRect();
		fire(d, 'dragover', {clientY: r.top + r.height * 0.1});
		fire(s, 'drop');
		fire(s, 'dragend');
		return true;
	})()`, nil)
	p.until("moved to the top", queueNames+`[0] === 'three'`)

	p.openRow(third)
	row := `.task[data-id="` + third + `"]`
	p.until("the reason offer", `document.querySelector('`+row+` .reason-link') && !document.querySelector('`+row+` .reason-link').hidden`)
	p.eval(`document.querySelector('`+row+` .reason-link').click()`, nil)
	p.until("the reason field", `document.activeElement === document.querySelector('`+row+` .panel-actions input[aria-label="理由"]')`)
	p.run(chromedp.SendKeys(row+` .panel-actions input[aria-label="理由"]`, "due soon", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("the offer gone", `!document.querySelector('`+row+` .panel-actions input[aria-label="理由"]') && document.querySelector('`+row+` .reason-link').hidden`)

	res, err := http.Get(a.base + "/api/tasks/" + third + "/context")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var ctxBody struct {
		Events []struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		} `json:"events"`
	}
	if err := json.NewDecoder(res.Body).Decode(&ctxBody); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ctxBody.Events {
		if e.Kind == "reordered" && e.Reason == "due soon" {
			found = true
		}
	}
	if !found {
		t.Errorf("no reordered event with the reason: %+v", ctxBody.Events)
	}
}

// What changed elsewhere in the field being typed in does not pour over the
// typed text; once the field is left, the typed text is what is saved.
func TestTodoDetailExternalChangeDoesNotOverwriteTypedText(t *testing.T) {
	a := startApp(t)
	_, second, p := openSecond(t, a)
	field := `document.querySelector('.task.row-open .live.detail')`
	p.until("detail field", field)

	g := p.holdList(fetch.RequestStageRequest)
	a.addTask(t, "fourth")
	g.waitHeld(t, 10*time.Second)
	p.run(chromedp.SendKeys(`.task.row-open .live.detail`, "typed", chromedp.ByQuery))
	a.patch(t, second, map[string]any{"detail": "from elsewhere"})
	g.release()
	p.until("list redrawn", queueHas(4, "fourth"))
	if got := p.str(field + `.textContent`); got != "typed" {
		t.Errorf("detail text = %q, want the typed text kept", got)
	}

	p.eval(`document.activeElement.blur()`, nil)
	a.untilTask(t, second, "the typed detail saved", func(m map[string]any) bool { return str(m, "detail") == "typed" })
}

// The parent picker stays in the open row: after a change it shows the task's
// own parent and is enabled again, so it can be changed once more without
// reopening the row; a refused change leaves it enabled on the real parent.
func TestTodoDetailParentPickerWorksAgainAndAfterAFailure(t *testing.T) {
	a := startApp(t)
	first, second, p := openSecond(t, a)
	sel := `.task.row-open .parent-pick select`
	set := func(v string) {
		p.eval(`(() => { const s = document.querySelector('`+sel+`'); s.value = '`+v+`'; s.dispatchEvent(new Event('change', {bubbles: true})); return true; })()`, nil)
	}
	settled := func(v string) string {
		return `document.querySelector('` + sel + `') && !document.querySelector('` + sel + `').disabled && document.querySelector('` + sel + `').value === '` + v + `'`
	}

	set(first)
	a.untilTask(t, second, "the parent", func(m map[string]any) bool { return str(m, "parent") == first })
	p.until("picker enabled on the parent", settled(first))
	set("")
	a.untilTask(t, second, "the parent cleared", func(m map[string]any) bool { return str(m, "parent") == "" })
	p.until("picker enabled on none", settled(""))

	// Writes to a task fail from here on (the request never reaches the server).
	chromedp.ListenTarget(p.ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			if e.Request.Method == http.MethodPatch {
				_ = chromedp.Run(p.ctx, fetch.FailRequest(e.RequestID, network.ErrorReasonFailed))
				return
			}
			_ = chromedp.Run(p.ctx, fetch.ContinueRequest(e.RequestID))
		}()
	})
	p.run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/api/tasks/*", RequestStage: fetch.RequestStageRequest}}))
	set(first)
	p.until("the refusal shown", `!document.getElementById('err').hidden`)
	p.until("picker enabled on the real parent", settled(""))
	if got := str(a.task(t, second), "parent"); got != "" {
		t.Errorf("parent after a refused change = %q, want none", got)
	}
}

// A related URL added from the page, and another added elsewhere before the
// next read: the rows show both, and the next edit from the page keeps the
// one added elsewhere.
func TestTodoDetailLinksTakeUpAnExternalChangeAfterASave(t *testing.T) {
	a := startApp(t)
	_, second, p := openSecond(t, a)
	linkCount := func(m map[string]any) int {
		links, _ := m["links"].([]any)
		return len(links)
	}

	p.click(`.task.row-open .link-add`)
	p.run(chromedp.SendKeys(`.task.row-open .link-row:last-child .link-input`, "https://example.com/one one", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, second, "the first URL", func(m map[string]any) bool { return linkCount(m) == 1 })

	a.patch(t, second, map[string]any{"links": []map[string]any{
		{"url": "https://example.com/one", "label": "one"},
		{"url": "https://example.com/elsewhere", "label": "elsewhere"},
	}})
	p.reload()
	p.until("both URLs shown", `document.querySelectorAll('.task.row-open .link-row').length === 2`)

	p.click(`.task.row-open .link-add`)
	p.run(chromedp.SendKeys(`.task.row-open .link-row:last-child .link-input`, "https://example.com/three three", chromedp.ByQuery))
	p.key(kb.Enter)
	a.untilTask(t, second, "three URLs", func(m map[string]any) bool { return linkCount(m) == 3 })
	links, _ := a.task(t, second)["links"].([]any)
	second2, _ := links[1].(map[string]any)
	if got := fmt.Sprint(second2["url"]); got != "https://example.com/elsewhere" {
		t.Errorf("second URL = %q, want the one added elsewhere kept", got)
	}
}

// A reply that moves the row to another section while a field has text not
// saved yet (the pause in typing is not over) must not drop it: the text is
// saved for the task it belongs to as the field goes.
func TestTodoDetailTypedTextIsSavedWhenTheRowGoes(t *testing.T) {
	cases := []struct {
		check     func(m map[string]any) bool
		name, sel string
	}{
		{func(m map[string]any) bool { return strings.Contains(str(m, "name"), "typed") }, "name", `.task.row-open .txt.live`},
		{func(m map[string]any) bool { return str(m, "detail") == "typed" }, "detail", `.task.row-open .live.detail`},
		{func(m map[string]any) bool { return str(m, "topic") == "typed" }, "topic", `.task.row-open .topic-input`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := startApp(t)
			_, second, p := openSecond(t, a)
			p.until("the field", `document.querySelector('`+c.sel+`')`)

			g := p.holdList(fetch.RequestStageRequest)
			a.addTask(t, "fourth")
			g.waitHeld(t, 10*time.Second)
			p.run(chromedp.SendKeys(c.sel, "typed", chromedp.ByQuery))
			// Some browsers send no blur when the focused element is removed:
			// swallow it here so this one behaves that way too.
			p.eval(`(() => {
				for (const type of ['blur', 'focusout']) {
					window.addEventListener(type, ev => ev.stopImmediatePropagation(), true);
				}
				return true;
			})()`, nil)
			// Its task goes to 待ち (a closed section) before the pause is over.
			a.post(t, "/api/tasks/"+second+"/wait", map[string]any{"to": "bob"})
			g.release()
			p.until("the row gone from the queue", `document.querySelectorAll('#queue .task.row-open').length === 0 && `+queueNames+`.includes('fourth')`)
			a.untilTask(t, second, "the typed "+c.name, c.check)
		})
	}
}
