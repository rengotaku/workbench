//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// page is a headless Chrome tab on the app.
type page struct {
	t   *testing.T
	ctx context.Context
}

// newPage starts a fresh browser (its own profile) and opens path.
func newPage(t *testing.T, a *app, path string) *page {
	t.Helper()
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.WindowSize(1280, 800))
	if p := os.Getenv("CHROME_PATH"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	if os.Getenv("CI") != "" {
		// A hosted runner restricts user namespaces, which Chrome's sandbox needs.
		opts = append(opts, chromedp.NoSandbox)
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(func() { cancelTimeout(); cancelCtx(); cancelAlloc() })

	p := &page{t: t, ctx: ctx}
	p.run(chromedp.Navigate(a.base + path))
	return p
}

func (p *page) run(actions ...chromedp.Action) {
	p.t.Helper()
	if err := chromedp.Run(p.ctx, actions...); err != nil {
		p.t.Fatalf("browser: %v", err)
	}
}

// eval runs a JS expression in the page and decodes its value into res.
func (p *page) eval(js string, res any) {
	p.t.Helper()
	p.run(chromedp.Evaluate(js, res))
}

// until polls a JS expression until it is truthy.
func (p *page) until(what, js string) {
	p.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
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

// click clicks the first element matching a CSS selector, as a mouse would.
func (p *page) click(sel string) {
	p.t.Helper()
	p.run(chromedp.Click(sel, chromedp.ByQuery))
}

// nav follows a header link (in the page when the router can). The todo
// screens hide the header nav and carry their own links in the sidebar, so from
// there the sidebar's link is the one followed.
func (p *page) nav(href string) {
	p.t.Helper()
	var navShown bool
	p.eval(`getComputedStyle(document.querySelector('.site-head')).display !== 'none'`, &navShown)
	if navShown {
		p.click(`.site-nav a[href="` + href + `"]`)
		return
	}
	p.click(`.side-links a[href="` + href + `"]`)
}

// key presses one key on whatever has focus.
func (p *page) key(k string) {
	p.t.Helper()
	p.run(chromedp.KeyEvent(k))
}

func (p *page) esc() { p.key(kb.Escape) }

func (p *page) str(js string) string {
	p.t.Helper()
	var s string
	p.eval(js, &s)
	return s
}

func (p *page) num(js string) float64 {
	p.t.Helper()
	var n float64
	p.eval(js, &n)
	return n
}

// texts returns the trimmed text of every element matching sel.
func (p *page) texts(sel string) []string {
	p.t.Helper()
	var out []string
	p.eval(`Array.from(document.querySelectorAll('`+sel+`')).map(e => e.textContent.trim())`, &out)
	return out
}

func (p *page) path() string { return p.str(`location.pathname`) }

// shown is a JS expression: the screen's root is on show.
func shown(view string) string {
	return `(r => r && !r.hidden)(document.querySelector('[data-view="` + view + `"]'))`
}
