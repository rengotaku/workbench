//go:build e2e

package e2e

import (
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

// gate holds back the first todo list read (GET /api/tasks?...) the page sends
// after holdList, until release. Every other request goes through.
type gate struct {
	p    *page
	held fetch.RequestID
	mu   sync.Mutex
	done bool // released: later reads pass
}

// holdList pauses the first list read from now on. stage says when: at the
// request (the server answers with whatever is true at release) or at the
// response (the server has already answered, so the reply is a stale copy by
// release). It also counts the list reads and marks each one once the page has
// handled it: window.__listHandled[n], n counted from 0 after this call.
func (p *page) holdList(stage fetch.RequestStage) *gate {
	p.t.Helper()
	g := &gate{p: p}
	chromedp.ListenTarget(p.ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		g.mu.Lock()
		take := g.held == "" && !g.done
		if take {
			g.held = e.RequestID
		}
		g.mu.Unlock()
		if take {
			return
		}
		go func() { _ = chromedp.Run(p.ctx, fetch.ContinueRequest(e.RequestID)) }()
	})
	p.run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*/api/tasks\\?*", RequestStage: stage}}))

	// The page's own handlers chain on res.json(), so they all run as
	// microtasks before the timer that marks the read as handled: waiting for
	// the mark replaces guessing a delay.
	p.eval(`(() => {
		window.__listHandled = {};
		let n = 0;
		const orig = window.fetch;
		window.fetch = function (url, opts) {
			const p = orig.apply(this, arguments);
			if (!String(url).includes('/api/tasks?')) return p;
			const mine = n++;
			return p.then(res => {
				const json = res.json.bind(res);
				res.json = () => json().finally(() => setTimeout(() => { window.__listHandled[mine] = true; }, 0));
				return res;
			});
		};
		return true;
	})()`, nil)
	return g
}

// waitHeld waits until a read has been paused.
func (g *gate) waitHeld(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		g.mu.Lock()
		got := g.held
		g.mu.Unlock()
		if got != "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the list read never went out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// release lets the held read through.
func (g *gate) release() {
	g.p.t.Helper()
	g.mu.Lock()
	id := g.held
	g.done = true
	g.mu.Unlock()
	g.p.run(fetch.ContinueRequest(id))
}

// untilWithin is until with a longer wait, for what only the 4 s poll brings.
func (p *page) untilWithin(d time.Duration, what, js string) {
	p.t.Helper()
	deadline := time.Now().Add(d)
	for {
		var ok bool
		p.eval("!!("+js+")", &ok)
		if ok {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("timed out waiting for: %s (%s)", what, js)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// reload makes the list read again, as coming back to the window does.
func (p *page) reload() {
	p.t.Helper()
	p.eval(`document.dispatchEvent(new Event('visibilitychange'))`, nil)
}

// done completes a task through the API (a change made elsewhere).
func (a *app) done(t *testing.T, id string) {
	t.Helper()
	a.post(t, "/api/tasks/"+id+"/done", map[string]any{})
}
