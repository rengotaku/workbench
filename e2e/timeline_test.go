//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// seedMemo is a memo written straight into notes.json, so a test can put memos
// on several days (the API stamps every memo with now).
type seedMemo struct {
	at     string // RFC3339
	text   string
	topic  string
	note   string      // an annotation (kind quote) on the memo
	links  [][2]string // URL, label; numbered [1], [2]… in order
	triage bool        // the AI has picked it up
}

// seedNotes writes notes.json for the given memos (ids from 1) and returns the
// data directory to start the server with.
func seedNotes(t *testing.T, topics []string, memos []seedMemo) string {
	t.Helper()
	notes := []map[string]any{}
	for i, m := range memos {
		links := []map[string]any{}
		for j, l := range m.links {
			links = append(links, map[string]any{"url": l[0], "label": l[1], "n": j + 1})
		}
		anns := []map[string]any{}
		if m.note != "" {
			anns = append(anns, map[string]any{"created_at": m.at, "kind": "quote", "body": m.note, "author": "ai", "id": i + 1})
		}
		n := map[string]any{
			"created_at": m.at, "text": m.text, "url": "", "topic": m.topic,
			"links": links, "annotations": anns, "id": i + 1,
		}
		if m.triage {
			n["triaged_at"] = m.at
		}
		notes = append(notes, n)
	}
	raw, err := json.Marshal(map[string]any{
		"notes": notes, "topics": topics, "next_note": len(memos) + 1, "next_annotation": len(memos) + 2, "schema_version": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// threeDays: five memos over three days, two topics.
func threeDays(t *testing.T) *app {
	return startAppWith(t, seedNotes(t, []string{"alpha", "beta"}, []seedMemo{
		{at: "2026-11-03T12:00:00Z", text: "first [1] and [2] but not [9]", topic: "alpha",
			links: [][2]string{{"https://example.com/one", "one"}, {"https://example.com/two", ""}}, triage: true},
		{at: "2026-11-04T12:00:00Z", text: "alpha-two", topic: "alpha", triage: true, note: "quoted body"},
		{at: "2026-11-04T12:01:00Z", text: "beta-one", topic: "beta"},
		{at: "2026-11-05T12:00:00Z", text: "no-topic-one", topic: ""},
		{at: "2026-11-05T12:01:00Z", text: "alpha-three", topic: "alpha"},
	}))
}

const (
	tlMemos   = `Array.from(document.querySelectorAll('#tl-days .memo')).map(e => e.textContent.trim())`
	tlChips   = `Array.from(document.querySelectorAll('#filters a.chip')).map(e => e.textContent.trim())`
	tlHeads   = `document.querySelectorAll('#tl-days section h2.day').length`
	tlPending = `document.getElementById('tl-pending').textContent`
)

// retype replaces the text of the input matching sel with text, by the keys.
func (p *page) retype(sel, text string) {
	p.t.Helper()
	p.eval(`document.querySelector('`+sel+`').select()`, nil)
	p.run(chromedp.SendKeys(sel, text, chromedp.ByQuery))
}

func (p *page) strs(js string) []string {
	p.t.Helper()
	var out []string
	p.eval(js, &out)
	return out
}

// The filter changes the list and the day groups, and the [N] in a memo is a
// link to its related URL.
func TestTimelineFilterDaysAndRefs(t *testing.T) {
	a := threeDays(t)
	p := newPage(t, a, "/notes/timeline")
	p.until("five memos", `document.querySelectorAll('#tl-days .memo').length === 5`)

	if got := p.num(tlHeads); got != 3 {
		t.Errorf("day groups = %v, want 3", got)
	}
	// Newest day first; the memos of a day are in the server's order.
	if got := p.num(`document.querySelectorAll('#tl-days section')[1].querySelectorAll('.ev').length`); got != 2 {
		t.Errorf("memos in the second group = %v, want 2", got)
	}
	if got := p.str(tlPending); got != "補足待ち 3" {
		t.Errorf("pending = %q", got)
	}
	if got := p.strs(tlChips); !reflect.DeepEqual(got, []string{"すべて", "alpha", "beta", "話題なし"}) {
		t.Errorf("chips = %v", got)
	}

	// [1] and [2] are links to the related URLs; [9] has no row and stays text.
	if got := p.strs(`Array.from(document.querySelectorAll('#note-1 .memo a.ref')).map(e => e.textContent + '>' + e.href + '>' + e.target + '>' + e.rel)`); !reflect.DeepEqual(got,
		[]string{"[1]>https://example.com/one>_blank>noopener noreferrer", "[2]>https://example.com/two>_blank>noopener noreferrer"}) {
		t.Errorf("refs = %v", got)
	}
	if got := p.str(`document.querySelector('#note-1 .memo').textContent`); got != "first [1] and [2] but not [9]" {
		t.Errorf("memo text = %q", got)
	}
	if got := p.strs(`Array.from(document.querySelectorAll('#note-1 ol.links li')).map(e => e.textContent)`); !reflect.DeepEqual(got, []string{"[1] リンク one", "[2] リンク https://example.com/two"}) {
		t.Errorf("related links = %v", got)
	}
	if got := p.str(`document.querySelector('#note-2 .add.k-quote blockquote.body').textContent`); got != "quoted body" {
		t.Errorf("annotation = %q", got)
	}
	if !p.bool(`document.querySelector('#note-3 .pending')`) || p.bool(`document.querySelector('#note-1 .pending')`) {
		t.Error("the waiting mark belongs to the memo the AI has not picked up")
	}

	p.click(`#filters a.chip[data-topic="alpha"]`)
	p.until("alpha only", `document.querySelectorAll('#tl-days .memo').length === 3 && location.search === '?topic=alpha'`)
	if got := p.num(tlHeads); got != 3 {
		t.Errorf("day groups under alpha = %v, want 3", got)
	}
	if got := p.texts(`#filters a.chip[aria-current="page"]`); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("current chip = %v", got)
	}
	if got := p.str(tlPending); got != "補足待ち 1" {
		t.Errorf("pending under alpha = %q", got)
	}

	p.click(`#filters a.chip[data-topic="beta"]`)
	p.until("beta only", `document.querySelectorAll('#tl-days .memo').length === 1`)
	if got := p.num(tlHeads); got != 1 {
		t.Errorf("day groups under beta = %v, want 1", got)
	}

	p.click(`#filters a.chip:not([data-topic]):last-of-type`)
	p.until("no topic", `location.search === '?no_topic=1' && document.querySelectorAll('#tl-days .memo').length === 1 && document.querySelector('#tl-days .memo').textContent.includes('no-topic-one')`)
	if got := p.texts(`#tl-days .memo`); !reflect.DeepEqual(got, []string{"no-topic-one"}) {
		t.Errorf("memos without a topic = %v", got)
	}

	p.click(`#filters a.chip:first-of-type`)
	p.until("all again", `document.querySelectorAll('#tl-days .memo').length === 5 && location.search === ''`)
}

// A filter with no memo says so.
func TestTimelineEmptyFilter(t *testing.T) {
	a := startAppWith(t, seedNotes(t, []string{"alpha", "beta"}, []seedMemo{
		{at: "2026-11-03T12:00:00Z", text: "only alpha", topic: "alpha"},
	}))
	p := newPage(t, a, "/notes/timeline?topic=beta")
	p.until("empty note", `document.querySelector('#tl-days p.empty')`)
	if got := p.str(`document.querySelector('#tl-days p.empty').textContent`); !strings.Contains(got, "この絞り込みに合うメモはありません") {
		t.Errorf("empty text = %q", got)
	}
}

// organizeRows is the topics the dialog lists, as "name:count".
const organizeRows = `Array.from(document.querySelectorAll('#organize-rows .row')).map(r => r.querySelector('.name').textContent + ':' + r.querySelector('.n').textContent)`

func (p *page) openOrganize() {
	p.t.Helper()
	p.click(`#organize`)
	p.until("dialog open with rows", `document.getElementById('organize-dialog').open && document.querySelectorAll('#organize-rows .row').length > 0`)
}

// rowButton clicks the named button of the row for a topic.
func (p *page) rowButton(topic, label string) {
	p.t.Helper()
	p.eval(fmt.Sprintf(`(() => {
		const row = Array.from(document.querySelectorAll('#organize-rows .row')).find(r => r.querySelector('.name').textContent === %q);
		Array.from(row.querySelectorAll('.acts button')).find(b => b.textContent === %q).click();
		return true;
	})()`, topic, label), nil)
}

// Renaming, merging and deleting a topic in the dialog change the list in the
// dialog at once, and the timeline's chips and memos when it closes.
func TestTimelineOrganizeRenameMergeDelete(t *testing.T) {
	a := threeDays(t)
	p := newPage(t, a, "/notes/timeline")
	p.until("five memos", `document.querySelectorAll('#tl-days .memo').length === 5`)

	p.openOrganize()
	if got := p.strs(organizeRows); !reflect.DeepEqual(got, []string{"alpha:3 件", "beta:1 件"}) {
		t.Fatalf("rows = %v", got)
	}

	// Rename: the box takes the focus, says what will happen, and typing an
	// existing name turns the message into the merge notice.
	p.rowButton("alpha", "名前を変える")
	p.until("rename box focused", `document.activeElement && document.activeElement.getAttribute('aria-label') === '新しい名前' && document.activeElement.value === 'alpha'`)
	if got := p.str(`document.querySelector('.edit .warn').textContent`); !strings.Contains(got, "3 件のメモも新しい名前になる") {
		t.Errorf("rename note = %q", got)
	}
	p.retype(`.edit input`, "beta")
	p.until("merge notice", `document.querySelector('.edit .warn').textContent.includes('既にある話題なので統合になる')`)
	// Same name: refused without a call.
	p.retype(`.edit input`, "alpha")
	p.click(`.edit .btn:not(.plain)`)
	p.until("same name refused", `document.querySelector('.edit .warn').textContent === '今と違う名前を入れてください'`)
	// Enter submits the new name.
	p.retype(`.edit input`, "gamma")
	p.key(kb.Enter)
	p.until("renamed in the dialog", `document.querySelectorAll('#organize-rows .row .name')[0].textContent === 'gamma'`)
	if got := p.strs(organizeRows); !reflect.DeepEqual(got, []string{"gamma:3 件", "beta:1 件"}) && !reflect.DeepEqual(got, []string{"beta:1 件", "gamma:3 件"}) {
		t.Errorf("rows after rename = %v", got)
	}
	if p.bool(`document.querySelector('.edit')`) {
		t.Error("the edit box stays after a successful rename")
	}

	// Merge beta into gamma.
	p.rowButton("beta", "統合")
	p.until("merge box", `document.querySelector('.edit select')`)
	if got := p.str(`document.querySelector('.edit .warn').textContent`); !strings.Contains(got, "「beta」の 1 件を「gamma」に付け替え") {
		t.Errorf("merge note = %q", got)
	}
	p.click(`.edit .btn:not(.plain)`)
	p.until("merged", `document.querySelectorAll('#organize-rows .row').length === 1`)
	if got := p.strs(organizeRows); !reflect.DeepEqual(got, []string{"gamma:4 件"}) {
		t.Errorf("rows after merge = %v", got)
	}

	// Closing reads the timeline again.
	p.click(`#organize-close`)
	p.until("dialog closed", `!document.getElementById('organize-dialog').open`)
	p.until("chips follow", `document.querySelectorAll('#filters a.chip[data-topic]').length === 1 && document.querySelector('#filters a.chip[data-topic]').dataset.topic === 'gamma'`)
	if got := p.num(`document.querySelectorAll('#tl-days .ev .topic').length`); got != 4 {
		t.Errorf("memos with a topic = %v, want 4", got)
	}
	if !p.bool(`Array.from(document.querySelectorAll('#tl-days .topic')).every(e => e.textContent === 'gamma')`) {
		t.Error("memos still show the old topic names")
	}

	// Delete: the memos become topic-less.
	p.openOrganize()
	p.rowButton("gamma", "削除")
	p.until("delete box", `document.querySelector('.edit.del')`)
	if got := p.str(`document.querySelector('.edit.del span').textContent`); !strings.Contains(got, "付いている 4 件のメモは話題なしになる") {
		t.Errorf("delete note = %q", got)
	}
	p.click(`.edit.del .btn.red`)
	p.until("deleted", `document.querySelector('#organize-rows .empty') && document.querySelector('#organize-rows .empty').textContent === '話題はまだありません'`)
	p.esc()
	p.until("closed by Esc", `!document.getElementById('organize-dialog').open`)
	p.until("no topics left", `document.querySelectorAll('#tl-days .memo').length === 5 && document.querySelectorAll('#tl-days .topic').length === 0 && document.querySelectorAll('#filters a.chip[data-topic]').length === 0`)
}

// A write that is still out when the dialog is closed must still make the
// timeline read again once it lands.
func TestTimelineOrganizeClosedWhileSaving(t *testing.T) {
	a := threeDays(t)
	p := newPage(t, a, "/notes/timeline")
	p.until("five memos", `document.querySelectorAll('#tl-days .memo').length === 5`)
	p.holdFetch("PUT", "/api/topics/beta")

	p.openOrganize()
	p.eval(`window.__hold = true`, nil)
	p.rowButton("beta", "名前を変える")
	p.run(chromedp.SetValue(`.edit input`, "delta", chromedp.ByQuery))
	p.click(`.edit .btn:not(.plain)`)
	p.esc()
	p.until("closed", `!document.getElementById('organize-dialog').open`)
	p.eval(`window.__release()`, nil)
	p.until("the timeline read again", `document.querySelector('#filters a.chip[data-topic="delta"]') && !document.querySelector('#filters a.chip[data-topic="beta"]')`)
}

// The list from an earlier open of the dialog, arriving after a later open
// has drawn, does not draw over it.
func TestTimelineOrganizeStaleListDropped(t *testing.T) {
	a := threeDays(t)
	p := newPage(t, a, "/notes/timeline")
	p.until("five memos", `document.querySelectorAll('#tl-days .memo').length === 5`)
	// The first GET /api/topics is held back; later ones go through.
	p.eval(`(() => {
		const orig = window.fetch;
		let n = 0, release;
		window.__release = () => release();
		window.fetch = function (url, opts) {
			const m = (opts && opts.method) || 'GET';
			if (m === 'GET' && String(url).split('?')[0].endsWith('/api/topics') && n++ === 0) {
				return orig.apply(this, arguments).then(res => new Promise(r => { release = () => r(res); }));
			}
			return orig.apply(this, arguments);
		};
		return true;
	})()`, nil)

	p.click(`#organize`) // the held read: [alpha, beta]
	p.until("dialog open", `document.getElementById('organize-dialog').open`)
	p.esc()
	p.until("closed", `!document.getElementById('organize-dialog').open`)

	req, err := http.NewRequest(http.MethodDelete, a.base+"/api/topics/beta", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	p.openOrganize() // the fresh read: [alpha]
	if got := p.strs(organizeRows); !reflect.DeepEqual(got, []string{"alpha:3 件"}) {
		t.Fatalf("fresh rows = %v", got)
	}
	p.eval(`window.__release()`, nil)
	time.Sleep(300 * time.Millisecond) // nothing to poll: the reply must change nothing
	if got := p.strs(organizeRows); !reflect.DeepEqual(got, []string{"alpha:3 件"}) {
		t.Errorf("rows after the old reply = %v, want only alpha", got)
	}
}

// The timeline keeps its scroll position and filter when you go to another
// screen and come back: by Back (the URL carries the filter) and by the tab.
func TestTimelineKeepsScrollAndFilterAcrossScreens(t *testing.T) {
	memos := []seedMemo{}
	for i := 0; i < 60; i++ {
		topic := "alpha"
		if i%3 == 0 {
			topic = "beta"
		}
		memos = append(memos, seedMemo{at: fmt.Sprintf("2026-10-%02dT12:00:00Z", 1+i/3), text: fmt.Sprintf("memo-%02d", i), topic: topic})
	}
	a := startAppWith(t, seedNotes(t, []string{"alpha", "beta"}, memos))
	p := newPage(t, a, "/notes/timeline")
	p.until("sixty memos", `document.querySelectorAll('#tl-days .memo').length === 60`)
	p.eval(`window.__marker = 'same-page'`, nil)

	p.click(`#filters a.chip[data-topic="alpha"]`)
	p.until("alpha", `document.querySelectorAll('#tl-days .memo').length === 40`)
	p.eval(`window.scrollTo(0, 600)`, nil)
	p.until("scrolled", `window.scrollY > 300`)
	scrolled := p.num(`window.scrollY`)

	p.nav("/notes")
	p.until("note add", shown("note-add"))
	p.eval(`history.back()`, nil)
	p.until("timeline again", shown("timeline")+` && location.search === '?topic=alpha'`)
	p.until("alpha still", `document.querySelectorAll('#tl-days .memo').length === 40`)
	if got := p.num(`window.scrollY`); math.Abs(got-scrolled) > 2 {
		t.Errorf("scrollY after Back = %v, want %v", got, scrolled)
	}
	if got := p.texts(`#filters a.chip[aria-current="page"]`); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("current chip after Back = %v", got)
	}

	// By the tab the link is /notes/timeline (no filter): the page is not
	// loaded again and the scroll comes back.
	p.click(`#filters a.chip:first-of-type`)
	p.until("all", `location.search === '' && document.querySelectorAll('#tl-days .memo').length === 60`)
	p.eval(`window.scrollTo(0, 900)`, nil)
	p.until("scrolled", `window.scrollY > 500`)
	scrolled = p.num(`window.scrollY`)
	p.nav("/notes")
	p.until("note add", shown("note-add"))
	p.nav("/notes/timeline")
	p.until("timeline by the tab", shown("timeline")+` && document.querySelectorAll('#tl-days .memo').length === 60`)
	if got := p.num(`window.scrollY`); math.Abs(got-scrolled) > 2 {
		t.Errorf("scrollY after the tab = %v, want %v", got, scrolled)
	}
	if got := p.str(`window.__marker`); got != "same-page" {
		t.Errorf("marker = %q: the page was loaded again", got)
	}
}

// The "他 N" input: typing narrows, ↑ wraps to the last candidate, Enter filters
// by it, and typing N in it does not open the add dialog.
func TestTimelineTopicPickerKeys(t *testing.T) {
	a := startApp(t)
	a.seedTopics(t, "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8")
	p := newPage(t, a, "/notes/timeline")
	p.until("more button", `!document.getElementById('chip-more').hidden`)

	p.click(`#chip-more`)
	p.until("input open", `document.activeElement && document.activeElement.classList.contains('topic-q')`)
	p.run(chromedp.SendKeys(`.topic-q`, "zz", chromedp.ByQuery))
	p.until("no candidate: the list is hidden", `document.querySelector('.suggest').hidden`)
	p.retype(`.topic-q`, "t")
	p.until("candidates", `document.querySelectorAll('.suggest li').length >= 1`)
	n := int(p.num(`document.querySelectorAll('.suggest li').length`))
	if !p.bool(`document.querySelector('.suggest li').classList.contains('hl')`) {
		t.Error("the first candidate starts highlighted")
	}
	p.key(kb.ArrowUp)
	last := p.str(fmt.Sprintf(`document.querySelectorAll('.suggest li')[%d].textContent`, n-1))
	if got := p.str(`document.querySelector('.suggest li.hl').textContent`); got != last {
		t.Errorf("highlight after ↑ = %q, want the last candidate %q", got, last)
	}
	if got := p.str(`document.querySelector('.topic-q').getAttribute('aria-activedescendant')`); got != p.str(`document.querySelector('.suggest li.hl').id`) {
		t.Errorf("aria-activedescendant = %q", got)
	}
	// Typing n is text for the input, not the shortcut.
	p.run(chromedp.SendKeys(`.topic-q`, "n", chromedp.ByQuery))
	p.until("n typed", `document.querySelector('.topic-q').value === 'tn'`)
	if p.bool(`document.querySelector('dialog[open]')`) {
		t.Error("N in the input opened the add dialog")
	}
	// The chips are the six newest topics (t8..t3); t1 and t2 are folded into "他 N".
	p.retype(`.topic-q`, "t1")
	p.key(kb.Enter)
	p.until("filtered by the pick", `location.search === '?topic=t1'`)
	if p.bool(`document.querySelector('.suggest-wrap')`) {
		t.Error("the input stays after a pick")
	}
}
