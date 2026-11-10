//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// pasteMemoLink pastes a copied link into the memo field of the note add screen.
func (p *page) pasteMemoLink(url, text string) {
	p.t.Helper()
	p.eval(fmt.Sprintf(`(() => {
		const node = document.getElementById('memo');
		node.focus();
		const dt = new DataTransfer();
		dt.setData('text/html', '<a href="%s">%s</a>');
		dt.setData('text/plain', %q);
		node.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
		return true;
	})()`, url, text, text), nil)
}

// pendingNotes reads the memos waiting for the AI.
func (a *app) pendingNotes(t *testing.T) []any {
	t.Helper()
	notes, _ := a.get(t, "/api/notes?pending=1")["notes"].([]any)
	return notes
}

// pendingNote returns the waiting memo with this text.
func (a *app) pendingNote(t *testing.T, text string) map[string]any {
	t.Helper()
	for _, x := range a.pendingNotes(t) {
		if n, _ := x.(map[string]any); str(n, "text") == text {
			return n
		}
	}
	t.Fatalf("no waiting memo %q", text)
	return nil
}

// seedTopics gives the add screen topics to offer, newest memo last.
func (a *app) seedTopics(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		a.addNote(t, "seed "+n, n)
		time.Sleep(5 * time.Millisecond)
	}
}

const notePressed = `Array.from(document.querySelectorAll('#topics .chip[data-topic][aria-pressed="true"]')).map(b => b.dataset.topic).join(',')`

// What is typed on the note add screen survives a draw: the memo and a row of
// related URLs are the same elements with the same text, and the caret stays,
// when a chip is pressed, a link is pasted (a row is added) or a row is removed.
func TestNoteAddKeepsInputAcrossRedraws(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "alpha", "beta")
	p := newPage(t, a, "/notes")
	p.until("chips", `document.querySelectorAll('#topics .chip[data-topic]').length === 2`)

	p.run(chromedp.SendKeys(`#memo`, "abcdef", chromedp.ByQuery))
	p.click(`#link-add`)
	p.run(chromedp.SendKeys(`.link-rows .link-input`, "https://example.com/a one", chromedp.ByQuery))
	p.eval(`document.getElementById('memo').focus()`, nil)
	p.until("row shown as a link", `document.querySelector('.link-rows .link-view') && !document.querySelector('.link-rows .link-view').hidden`)
	p.eval(`(() => {
		document.getElementById('memo').__kept = 1;
		document.querySelector('.link-rows .link-input').__kept = 1;
		document.querySelector('#topics .chip[data-topic]').__kept = 1;
		const memo = document.getElementById('memo');
		memo.focus();
		memo.setSelectionRange(3, 3);
		return true;
	})()`, nil)

	// A link pasted at the caret: the text goes in followed by [2], the URL
	// becomes the second row, and the memo keeps the focus.
	p.pasteMemoLink("https://example.com/b", "TICKET-2")
	p.until("second row", `document.querySelectorAll('.link-rows .link-row').length === 2`)
	if got := p.str(`document.getElementById('memo').value`); got != "abcTICKET-2 [2]def" {
		t.Errorf("memo after the paste = %q", got)
	}
	// Pressing a chip draws again and hands the focus back to the memo.
	p.click(`#topics .chip[data-topic="beta"]`)
	p.until("beta pressed", `document.querySelector('#topics .chip[data-topic="beta"]').getAttribute('aria-pressed') === 'true'`)

	for _, c := range []struct{ what, js string }{
		{"memo element and text", `document.getElementById('memo').__kept === 1 && document.getElementById('memo').value === 'abcTICKET-2 [2]def'`},
		{"first row element and text", `document.querySelector('.link-rows .link-input').__kept === 1 && document.querySelector('.link-rows .link-input').value === 'https://example.com/a one'`},
		{"chip element", `document.querySelector('#topics .chip[data-topic]').__kept === 1`},
		{"memo has the focus", `document.activeElement === document.getElementById('memo')`},
		{"caret kept where the paste left it", `document.getElementById('memo').selectionStart === 'abcTICKET-2 [2]'.length`},
	} {
		if !p.bool(c.js) {
			t.Errorf("after the draws: %s lost", c.what)
		}
	}
}

// Moving to another screen and back leaves the half-written memo, its topic and
// its related-URL rows as they were, in the same page.
func TestNoteAddKeepsDraftAcrossNavigation(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "alpha", "beta")
	p := newPage(t, a, "/notes")
	p.until("chips", `document.querySelectorAll('#topics .chip[data-topic]').length === 2`)
	p.eval(`window.__marker = 'same-page'`, nil)

	p.run(chromedp.SendKeys(`#memo`, "書きかけのメモ", chromedp.ByQuery))
	p.click(`#topics .chip[data-topic="alpha"]`)
	p.click(`#link-add`)
	p.run(chromedp.SendKeys(`.link-rows .link-input`, "https://example.com/draft 下書き", chromedp.ByQuery))
	p.click(`#link-add`)
	p.run(chromedp.SendKeys(`.link-rows .link-row:last-child .link-input`, "https://example.com/second", chromedp.ByQuery))
	p.eval(`document.getElementById('memo').__kept = 1`, nil)

	p.nav("/notes/timeline")
	p.until("timeline shown", shown("timeline"))
	p.nav("/")
	p.until("todo shown", shown("todo"))
	p.nav("/notes")
	p.until("note add shown again", shown("note-add"))

	if got := p.str(`window.__marker`); got != "same-page" {
		t.Errorf("window marker = %q: the page was loaded again", got)
	}
	if !p.bool(`document.getElementById('memo').__kept === 1`) {
		t.Error("the memo element was replaced")
	}
	if got := p.str(`document.getElementById('memo').value`); got != "書きかけのメモ" {
		t.Errorf("memo = %q, want it kept", got)
	}
	if got := p.str(notePressed); got != "alpha" {
		t.Errorf("picked topic = %q, want alpha", got)
	}
	got := p.str(`Array.from(document.querySelectorAll('.link-rows .link-input')).map(i => i.value).join('|')`)
	if got != "https://example.com/draft 下書き|https://example.com/second" {
		t.Errorf("rows = %q, want both kept", got)
	}
	if !p.bool(`document.activeElement === document.getElementById('memo')`) {
		t.Error("the memo does not have the focus on coming back")
	}
	// What is kept is what gets saved.
	p.key(kb.Enter)
	p.until("saved", `Array.from(document.querySelectorAll('#recent li')).some(li => li.textContent.includes('書きかけのメモ'))`)
	n := a.pendingNote(t, "書きかけのメモ")
	if str(n, "topic") != "alpha" {
		t.Errorf("saved note = %v", n)
	}
	if l, _ := n["links"].([]any); len(l) != 2 {
		t.Errorf("saved links = %v, want 2", n["links"])
	}
}

// The topic input: ↑ ↓ move the highlight (and the screen reader's active
// option), Enter picks, Esc closes and gives the focus back to the memo.
func TestNoteAddTopicPickerKeys(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "alpha", "beta", "gamma")
	p := newPage(t, a, "/notes")
	p.until("chips", `document.querySelectorAll('#topics .chip[data-topic]').length === 3`)

	p.click(`#topic-add`)
	p.until("input open", `document.activeElement && document.activeElement.classList.contains('topic-q')`)
	if !p.bool(`document.getElementById('topic-add').hidden`) {
		t.Error("the button stays beside the open input")
	}
	// An empty input offers every topic, newest first; the first is highlighted.
	if got := p.texts(`.suggest li`); strings.Join(got, ",") != "alpha,beta,gamma" {
		t.Errorf("suggestions = %v", got)
	}
	hl := `Array.from(document.querySelectorAll('.suggest li')).findIndex(li => li.classList.contains('hl'))`
	if got := p.num(hl); got != 0 {
		t.Errorf("highlight = %v, want 0", got)
	}
	p.key(kb.ArrowDown)
	p.key(kb.ArrowDown)
	if got := p.num(hl); got != 2 {
		t.Errorf("highlight after ↓↓ = %v, want 2", got)
	}
	p.key(kb.ArrowDown) // wraps
	if got := p.num(hl); got != 0 {
		t.Errorf("highlight after the third ↓ = %v, want 0 (wraps)", got)
	}
	p.key(kb.ArrowUp)
	if got := p.num(hl); got != 2 {
		t.Errorf("highlight after ↑ = %v, want 2 (wraps)", got)
	}
	// The input says which option is current, and the option says it is selected.
	if !p.bool(`(() => {
		const input = document.querySelector('.topic-q');
		const opt = document.getElementById(input.getAttribute('aria-activedescendant'));
		return opt && opt.getAttribute('role') === 'option' && opt.getAttribute('aria-selected') === 'true'
			&& input.getAttribute('aria-controls') === document.querySelector('.suggest').id;
	})()`) {
		t.Error("aria-activedescendant / aria-controls do not point at the highlighted option")
	}

	// Typing narrows by partial match.
	p.run(chromedp.SendKeys(`.topic-q`, "mm", chromedp.ByQuery))
	if got := p.texts(`.suggest li`); strings.Join(got, ",") != "gamma,「mm」を新しい話題として作る" {
		t.Errorf("suggestions after typing = %v", got)
	}
	p.key(kb.Enter) // gamma, highlighted first
	p.until("picker closed", `!document.querySelector('.suggest-wrap')`)
	if got := p.str(notePressed); got != "gamma" {
		t.Errorf("picked = %q, want gamma", got)
	}
	if p.bool(`document.getElementById('topic-add').hidden`) {
		t.Error("the button did not come back")
	}
	if !p.bool(`document.activeElement === document.getElementById('memo')`) {
		t.Error("the memo does not have the focus after a pick")
	}

	// Esc closes without picking, and does not leave the screen.
	p.click(`#topic-add`)
	p.until("input open", `document.querySelector('.topic-q')`)
	p.esc()
	p.until("picker closed by Esc", `!document.querySelector('.suggest-wrap')`)
	if got := p.str(notePressed); got != "gamma" {
		t.Errorf("Esc changed the pick: %q", got)
	}
	if got := p.path(); got != "/notes" {
		t.Errorf("path after Esc = %q: Esc in the input must not leave the screen", got)
	}
	if !p.bool(`document.activeElement === document.getElementById('memo')`) {
		t.Error("the memo does not have the focus after Esc")
	}
	// Esc with the focus on the memo leaves for the timeline.
	p.esc()
	p.until("timeline", `location.pathname === '/notes/timeline'`)
}

// A name that is not a topic can be made from the input: the topic is created
// on the server, gets a chip and is picked.
func TestNoteAddCreatesATopic(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "alpha")
	p := newPage(t, a, "/notes")
	p.until("chip", `document.querySelectorAll('#topics .chip[data-topic]').length === 1`)

	p.click(`#topic-add`)
	p.until("input open", `document.querySelector('.topic-q')`)
	p.run(chromedp.SendKeys(`.topic-q`, "fresh", chromedp.ByQuery))
	if got := p.texts(`.suggest li`); strings.Join(got, ",") != "「fresh」を新しい話題として作る" {
		t.Fatalf("suggestions = %v", got)
	}
	p.key(kb.Enter)
	p.until("picked", `document.querySelector('#topics .chip[data-topic="fresh"][aria-pressed="true"]') && !document.querySelector('.suggest-wrap')`)
	topics, _ := a.get(t, "/api/topics")["topics"].([]any)
	if !strings.Contains(fmt.Sprint(topics), "fresh") {
		t.Errorf("topics on the server = %v, want fresh among them", topics)
	}
	// A save carries the new topic.
	p.run(chromedp.SendKeys(`#memo`, "with a new topic", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("saved", `Array.from(document.querySelectorAll('#recent li')).some(li => li.textContent.includes('with a new topic'))`)
	if n := a.pendingNote(t, "with a new topic"); str(n, "topic") != "fresh" {
		t.Errorf("saved topic = %q, want fresh", str(n, "topic"))
	}
}

// While a new topic is being created the input is still there and Esc closes
// it; the topic is made all the same and gets its chip, but is not picked.
func TestNoteAddEscClosesWhileATopicIsBeingCreated(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "alpha")
	p := newPage(t, a, "/notes")
	p.until("chip", `document.querySelectorAll('#topics .chip[data-topic]').length === 1`)
	// Hold the answer of the topic creation until __release().
	p.eval(`(() => {
		const orig = window.fetch;
		let release;
		const gate = new Promise(r => { release = r; });
		window.__release = () => release();
		window.fetch = function (url, opts) {
			if (opts && opts.method === 'POST' && String(url).endsWith('/api/topics')) {
				return orig.apply(this, arguments).then(res => gate.then(() => res));
			}
			return orig.apply(this, arguments);
		};
		return true;
	})()`, nil)

	p.click(`#topic-add`)
	p.until("input open", `document.querySelector('.topic-q')`)
	p.run(chromedp.SendKeys(`.topic-q`, "slow", chromedp.ByQuery))
	p.key(kb.Enter)
	waitTopic(t, a, "slow")
	p.esc()
	p.until("input closed while the answer is out", `!document.querySelector('.suggest-wrap')`)
	p.eval(`window.__release()`, nil)
	p.until("chip made", `document.querySelector('#topics .chip[data-topic="slow"]')`)
	if got := p.str(notePressed); got != "" {
		t.Errorf("picked = %q: a topic whose input was closed must not be picked", got)
	}
}

func waitTopic(t *testing.T, a *app, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		topics, _ := a.get(t, "/api/topics")["topics"].([]any)
		if strings.Contains(fmt.Sprint(topics), name) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("topic %q never reached the server", name)
}

// A link pasted into the memo becomes "text [N]" with its URL as row N; a row
// the memo points at cannot be removed, and removing one renumbers the rest.
func TestNoteAddLinkPasteAndRemove(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/notes")
	p.until("screen", `document.getElementById('memo')`)

	p.pasteMemoLink("https://example.com/1", "one")
	p.pasteMemoLink("https://example.com/2", "two")
	p.until("two rows", `document.querySelectorAll('.link-rows .link-row').length === 2`)
	if got := p.str(`document.getElementById('memo').value`); got != "one [1]two [2]" {
		t.Fatalf("memo = %q, want each paste to carry its own number", got)
	}
	nums := `Array.from(document.querySelectorAll('.link-rows .link-row .ln')).map(e => e.textContent).join(',')`
	if got := p.str(nums); got != "[1],[2]" {
		t.Errorf("row numbers = %q", got)
	}
	// [1] is pointed at, so removing row 1 is refused with a message.
	p.eval(`document.querySelectorAll('.link-rows .link-remove')[0].click()`, nil)
	p.until("refused", `!document.getElementById('status').hidden`)
	if got := p.str(`document.getElementById('status').textContent`); !strings.Contains(got, "本文が [1] を指しているので [1] は外せません") {
		t.Errorf("status = %q", got)
	}
	if got := p.num(`document.querySelectorAll('.link-rows .link-row').length`); got != 2 {
		t.Errorf("rows = %v, want the row kept", got)
	}
	// Point the memo at nothing, then remove row 1: the rest is renumbered.
	p.eval(`document.getElementById('memo').value = 'plain'`, nil)
	p.eval(`document.querySelectorAll('.link-rows .link-remove')[0].click()`, nil)
	p.until("one row", `document.querySelectorAll('.link-rows .link-row').length === 1`)
	if got := p.str(nums); got != "[1]" {
		t.Errorf("row numbers after the removal = %q, want [1]", got)
	}
	if got := p.str(`document.querySelector('.link-rows .link-view').href`); got != "https://example.com/2" {
		t.Errorf("remaining row = %q, want the second URL", got)
	}
	if !p.bool(`document.getElementById('status').hidden`) {
		t.Error("the refusal stayed after the removal")
	}
	if !p.bool(`document.activeElement === document.getElementById('memo')`) {
		t.Error("the memo does not have the focus after a removal")
	}
	// An empty row stops the save and says which one.
	p.click(`#link-add`)
	p.key(kb.Enter)
	p.until("empty row refused", `!document.getElementById('status').hidden`)
	if got := p.str(`document.getElementById('status').textContent`); !strings.Contains(got, "関連 URL [2] が空です") {
		t.Errorf("status = %q", got)
	}
	if len(a.pendingNotes(t)) != 0 {
		t.Error("a memo with an empty row was saved")
	}
	// Row 2 is typed in (the focus is in it), and the memo saves with both.
	p.run(chromedp.SendKeys(`.link-rows .link-row:last-child .link-input`, "https://example.com/3 three", chromedp.ByQuery))
	p.key(kb.Enter)
	p.until("saved", `document.querySelectorAll('#recent li').length === 1`)
	notes := a.pendingNotes(t)
	n, _ := notes[0].(map[string]any)
	l, _ := n["links"].([]any)
	if len(l) != 2 {
		t.Fatalf("saved links = %v", n["links"])
	}
	if first, _ := l[0].(map[string]any); str(first, "url") != "https://example.com/2" {
		t.Errorf("first saved link = %v", first)
	}
	if second, _ := l[1].(map[string]any); str(second, "url") != "https://example.com/3" || str(second, "label") != "three" {
		t.Errorf("second saved link = %v", second)
	}
	// The fields are emptied for the next one.
	if got := p.str(`document.getElementById('memo').value`); got != "" {
		t.Errorf("memo after the save = %q", got)
	}
	if got := p.num(`document.querySelectorAll('.link-rows .link-row').length`); got != 0 {
		t.Errorf("rows after the save = %v", got)
	}
}

// A saved memo shows once on the waiting list with its [N] as a link; 削除
// strikes it through and stops counting it.
func TestNoteAddSaveAndDelete(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/notes")
	p.until("screen", `document.getElementById('memo')`)
	p.pasteMemoLink("https://example.com/x", "ticket")
	p.until("a row", `document.querySelectorAll('.link-rows .link-row').length === 1`)
	p.key(kb.Enter)
	p.until("one row", `document.querySelectorAll('#recent li').length === 1`)
	if got := p.str(`document.getElementById('pending-title').textContent`); got != "補足待ち 1 件" {
		t.Errorf("title = %q", got)
	}
	if got := p.str(`document.querySelector('#recent li a.ref').textContent + ' ' + document.querySelector('#recent li a.ref').href`); got != "[1] https://example.com/x" {
		t.Errorf("the memo's [1] link = %q", got)
	}
	if !p.bool(`document.querySelector('#recent li').classList.contains('new')`) {
		t.Error("the new row is not marked")
	}
	p.click(`#recent li .danger`)
	p.until("struck through", `document.querySelector('#recent li.is-undone .gone')`)
	if got := p.str(`document.getElementById('pending-title').textContent`); got != "補足待ち 0 件" {
		t.Errorf("title after the delete = %q", got)
	}
	if len(a.pendingNotes(t)) != 0 {
		t.Error("the memo is still waiting on the server")
	}
	// Coming back from another screen drops the deleted row and shows the rest.
	a.addNote(t, "from elsewhere", "")
	p.nav("/notes/timeline")
	p.until("timeline", shown("timeline"))
	p.nav("/notes")
	p.until("list matches the server", `document.querySelectorAll('#recent li').length === 1 && document.querySelector('#recent li').textContent.includes('from elsewhere')`)
}

// The memo a list read and a save both bring is one row, however the replies
// are ordered: here the list answers while the save's answer is still out.
func TestNoteAddSavedMemoIsOneRowWhateverLandsFirst(t *testing.T) {
	a := startApp(t)
	p := newPage(t, a, "/notes")
	p.until("screen", `document.getElementById('memo')`)
	p.eval(`(() => {
		const orig = window.fetch;
		let release;
		const gate = new Promise(r => { release = r; });
		window.__release = () => release();
		window.fetch = function (url, opts) {
			if (opts && opts.method === 'POST' && String(url).endsWith('/api/notes')) {
				return orig.apply(this, arguments).then(res => gate.then(() => res));
			}
			return orig.apply(this, arguments);
		};
		return true;
	})()`, nil)

	p.run(chromedp.SendKeys(`#memo`, "once", chromedp.ByQuery))
	p.key(kb.Enter)
	deadline := time.Now().Add(5 * time.Second)
	for len(a.pendingNotes(t)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the save never reached the server")
		}
		time.Sleep(30 * time.Millisecond)
	}
	// The screen is shown again (the list is read) while the save's answer is out.
	p.nav("/notes/timeline")
	p.until("timeline", shown("timeline"))
	p.nav("/notes")
	p.until("the list brought the memo", `document.querySelectorAll('#recent li').length === 1`)
	p.eval(`window.__release()`, nil)
	p.until("the save's answer handled", `document.getElementById('memo').value === '' && !document.getElementById('memo').disabled`)
	time.Sleep(200 * time.Millisecond)
	if got := p.num(`document.querySelectorAll('#recent li').length`); got != 1 {
		t.Errorf("rows = %v, want the memo once", got)
	}
	if got := p.str(`document.getElementById('pending-title').textContent`); got != "補足待ち 1 件" {
		t.Errorf("title = %q", got)
	}
}

// The timeline's topic input (the same picker, drawn for a plain-JS screen)
// opens from "他 N", narrows, picks by the keys and closes with Esc.
func TestTimelineTopicPickerStillWorks(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8")
	p := newPage(t, a, "/notes/timeline")
	p.until("more button", `!document.getElementById('chip-more').hidden`)

	p.click(`#chip-more`)
	p.until("input open", `document.activeElement && document.activeElement.classList.contains('topic-q')`)
	if !p.bool(`document.getElementById('chip-more').hidden`) {
		t.Error("the button stays beside the open input")
	}
	p.esc()
	p.until("closed by Esc", `!document.querySelector('.suggest-wrap') && !document.getElementById('chip-more').hidden`)
	if got := p.path(); got != "/notes/timeline" {
		t.Errorf("path after Esc = %q", got)
	}

	p.click(`#chip-more`)
	p.until("input open", `document.querySelector('.topic-q')`)
	p.run(chromedp.SendKeys(`.topic-q`, "t", chromedp.ByQuery))
	if n := p.num(`document.querySelectorAll('.suggest li').length`); n < 1 {
		t.Fatalf("suggestions = %v", n)
	}
	p.key(kb.ArrowDown)
	hl := p.str(`document.querySelector('.suggest li.hl').textContent`)
	p.key(kb.Enter)
	p.until("filtered", `location.search === '?topic=' + encodeURIComponent('`+hl+`')`)
	if !p.bool(`!document.querySelector('.suggest-wrap')`) {
		t.Error("the input is still there after a pick")
	}
}
