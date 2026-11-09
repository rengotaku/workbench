//go:build e2e

package e2e

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// A timeline reply that arrives late, after a newer filter has drawn, must not
// draw over it. The reply for ?topic=alpha is held back while the filter moves
// on to beta, then let through.
func TestTimelineLateReplyDoesNotOverwriteNewerFilter(t *testing.T) {
	a := startApp(t)
	a.addNote(t, "alpha-memo", "alpha")
	a.addNote(t, "beta-memo", "beta")

	p := newPage(t, a, "/notes/timeline")
	p.until("both memos", `document.querySelectorAll('#tl-days .memo').length === 2`)

	var (
		mu   sync.Mutex
		held fetch.RequestID
	)
	chromedp.ListenTarget(p.ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		hold := strings.Contains(e.Request.URL, "/api/notes/timeline") && strings.Contains(e.Request.URL, "topic=alpha")
		if hold {
			mu.Lock()
			held = e.RequestID
			mu.Unlock()
			return
		}
		go func() { _ = chromedp.Run(p.ctx, fetch.ContinueRequest(e.RequestID)) }()
	})
	p.run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/api/notes/timeline*"}}))

	// Mark when the page has handled the alpha reply: the page's own handlers
	// chain on res.json(), so they all run as microtasks before this timer.
	// The check below waits for the mark instead of guessing a delay.
	p.eval(`(() => {
		const orig = window.fetch;
		window.fetch = function (url, opts) {
			const p = orig.apply(this, arguments);
			if (!String(url).includes('topic=alpha')) return p;
			return p.then(res => {
				const json = res.json.bind(res);
				res.json = () => json().finally(() => setTimeout(() => { window.__alphaHandled = true; }, 0));
				return res;
			});
		};
		return true;
	})()`, nil)

	p.click(`#filters a.chip[data-topic="alpha"]`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := held
		mu.Unlock()
		if got != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the alpha request never went out")
		}
		time.Sleep(20 * time.Millisecond)
	}

	p.click(`#filters a.chip[data-topic="beta"]`)
	p.until("beta drawn", `document.querySelectorAll('#tl-days .memo').length === 1 && document.querySelector('#tl-days .memo').textContent.includes('beta-memo')`)

	// Let the old reply through and wait until the page has handled it.
	mu.Lock()
	id := held
	mu.Unlock()
	p.run(fetch.ContinueRequest(id))
	p.until("the late reply handled", `window.__alphaHandled === true`)

	if got := p.texts(`#tl-days .memo`); len(got) != 1 || !strings.Contains(got[0], "beta-memo") {
		t.Errorf("memos after the late reply = %v, want only beta-memo", got)
	}
	if got := p.str(`location.search`); got != "?topic=beta" {
		t.Errorf("query = %q, want ?topic=beta", got)
	}
	if got := p.texts(`#filters a.chip[aria-current="page"]`); len(got) != 1 || got[0] != "beta" {
		t.Errorf("current chip = %v, want beta", got)
	}
}
