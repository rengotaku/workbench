package pages

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/note-stock/internal/note"
	"github.com/rengotaku/note-stock/web"
)

const testPort = "8086"

func newPages(t *testing.T) (http.Handler, *note.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := LoadTemplates(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		t.Fatal(err)
	}
	return New(templates, static, store, testPort).Routes(), store
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:" + testPort
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func mustContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("page should contain %q", w)
		}
	}
}

func mustNotContain(t *testing.T, body string, nots ...string) {
	t.Helper()
	for _, w := range nots {
		if strings.Contains(body, w) {
			t.Errorf("page must not contain %q", w)
		}
	}
}

func TestAddPage(t *testing.T) {
	h, store := newPages(t)
	_, _ = store.Add("a", "青分離")
	_ = store.AddTopic("C&C")

	code, body := get(t, h, "/")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	mustContain(t, body, `id="memo"`, "autofocus", `data-topic="青分離"`, `data-topic="C&amp;C"`, "＋ 話題を追加", "/static/js/add.js")
	mustNotContain(t, body, "https://fonts.", "cdn.")
}

func TestTimelineGroupsByDayNewestFirst(t *testing.T) {
	h, store := newPages(t)
	// The store stamps notes with the real clock, so group by the dates it produced.
	_, _ = store.Add("古いメモ", "")
	_, _ = store.Add("新しいメモ", "")

	_, body := get(t, h, "/timeline")
	if strings.Index(body, "新しいメモ") > strings.Index(body, "古いメモ") {
		t.Error("newest memo should come first")
	}
	today := time.Now().Format("1/2")
	if got := strings.Count(body, `class="day"`); got != 1 {
		t.Errorf("both memos were added today, want one day heading, got %d", got)
	}
	mustContain(t, body, today+"（")
}

func TestBuildTimelineDayHeadings(t *testing.T) {
	loc := time.FixedZone("JST", 9*3600)
	notes := []note.Note{
		{ID: 3, Text: "c", CreatedAt: time.Date(2026, 10, 6, 16, 45, 0, 0, loc)},
		{ID: 2, Text: "b", CreatedAt: time.Date(2026, 10, 6, 11, 20, 0, 0, loc)},
		{ID: 1, Text: "a", CreatedAt: time.Date(2026, 10, 2, 10, 5, 0, 0, loc), Annotations: []note.Annotation{{ID: 1, Kind: note.KindMemo, Author: note.AuthorAI, Body: "x", CreatedAt: time.Date(2026, 10, 2, 10, 20, 0, 0, loc)}}},
	}
	v := buildTimeline(notes, nil, "", false)
	if len(v.Days) != 2 || len(v.Days[0].Notes) != 2 || len(v.Days[1].Notes) != 1 {
		t.Fatalf("days = %+v", v.Days)
	}
	if v.Pending != 2 {
		t.Errorf("Pending = %d, want 2", v.Pending)
	}
	if v.Days[0].Notes[0].ID != 3 || v.Days[1].Notes[0].Annotations[0].Label != "メモ" || v.Days[1].Notes[0].Annotations[0].Author != "AI" {
		t.Errorf("views = %+v", v.Days)
	}
}

func TestTimelineShowsLinksSourceAndAnnotations(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add("https://fundinno-inc.slack.com/archives/C1/p1 staging が変わった", "青分離")
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindQuote, Body: "剣持: 変更します\n・deploy_staging", Source: "slack"})
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindAnalysis, Body: "考察の本文", Author: note.AuthorHuman})
	_, _ = store.Add("補足のないメモ", "")

	_, body := get(t, h, "/timeline")
	mustContain(t, body,
		`<a href="https://fundinno-inc.slack.com/archives/C1/p1" target="_blank" rel="noopener noreferrer">`,
		`<span class="src">Slack</span>`,
		`<blockquote class="body">剣持: 変更します`,
		"引用 · AI", "考察 · 人", " · slack",
		"補足待ち 1",
		`<div class="pending">補足待ち</div>`,
	)
}

func TestTimelineEscapesUserContent(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add(`<script>alert(1)</script> https://example.com/?a=1&b="2"`, `<b>話題</b>`)
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindAnalysis, Body: `<img src=x onerror=alert(2)>`})

	_, body := get(t, h, "/timeline")
	mustNotContain(t, body, "<script>alert(1)", "<img src=x", "<b>話題</b>")
	mustContain(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;img src=x onerror=alert(2)&gt;")
}

func TestTimelineFilters(t *testing.T) {
	h, store := newPages(t)
	_, _ = store.Add("青のメモ", "青分離")
	_, _ = store.Add("CCのメモ", "C&C")
	_, _ = store.Add("無印のメモ", "")

	_, all := get(t, h, "/timeline")
	mustContain(t, all, "青のメモ", "CCのメモ", "無印のメモ", `href="/timeline?topic=C%26C"`, `href="/timeline?no_topic=1"`)
	mustContain(t, all, `class="chip on" href="/timeline" aria-current="page"`)

	_, blue := get(t, h, "/timeline?topic=%E9%9D%92%E5%88%86%E9%9B%A2")
	mustContain(t, blue, "青のメモ", `class="chip on" href="/timeline?topic=`)
	mustNotContain(t, blue, "CCのメモ", "無印のメモ")

	_, none := get(t, h, "/timeline?no_topic=1")
	mustContain(t, none, "無印のメモ")
	mustNotContain(t, none, "青のメモ", "CCのメモ")

	_, nobody := get(t, h, "/timeline?topic=nope")
	mustContain(t, nobody, "この絞り込みに合うメモはありません")
}

func TestTimelineEmpty(t *testing.T) {
	h, _ := newPages(t)
	_, body := get(t, h, "/timeline")
	mustContain(t, body, "まだメモがありません", "補足待ち 0")
}

func TestStaticAndHealth(t *testing.T) {
	h, _ := newPages(t)
	for _, p := range []string{"/static/css/style.css", "/static/js/shared.js", "/static/js/add.js", "/static/js/timeline.js", "/static/icon.svg"} {
		if code, body := get(t, h, p); code != http.StatusOK || body == "" {
			t.Errorf("%s: status %d, %d bytes", p, code, len(body))
		}
	}
	if code, body := get(t, h, "/healthz"); code != http.StatusOK || !strings.Contains(body, "ok") {
		t.Errorf("healthz = %d %s", code, body)
	}
	if code, _ := get(t, h, "/api/topics"); code != http.StatusOK {
		t.Errorf("API should be mounted beside the pages, status %d", code)
	}
}

func TestRenderErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// No templates at all: the page says so instead of panicking.
	h := New(map[string]*template.Template{}, fstest.MapFS{}, store, testPort).Routes()
	if code, _ := get(t, h, "/"); code != http.StatusInternalServerError {
		t.Errorf("missing template status = %d", code)
	}

	// A template that fails while executing ends in an error text, not a panic.
	broken := template.Must(template.New("base").Parse(`{{.NoSuchField}}`))
	h = New(map[string]*template.Template{"add.html": broken}, fstest.MapFS{}, store, testPort).Routes()
	if _, body := get(t, h, "/"); !strings.Contains(body, "画面を描画できませんでした") {
		t.Errorf("body = %q", body)
	}
}

func TestLoadTemplatesError(t *testing.T) {
	if _, err := LoadTemplates(fstest.MapFS{}); err == nil {
		t.Error("LoadTemplates should fail when the files are missing")
	}
}

func send(h http.Handler, method, path, host string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGuard(t *testing.T) {
	h, store := newPages(t)
	_, _ = store.Add("a", "")
	good := "127.0.0.1:" + testPort
	js := map[string]string{"Content-Type": "application/json"}
	jsCharset := map[string]string{"Content-Type": "application/json; charset=utf-8"}

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		hdr    map[string]string
		body   string
		want   int
	}{
		{"loopback host", "GET", "/timeline", good, nil, "", 200},
		{"localhost host", "GET", "/timeline", "localhost:" + testPort, nil, "", 200},
		{"foreign host (DNS rebinding)", "GET", "/timeline", "evil.example:" + testPort, nil, "", 403},
		{"wrong port", "GET", "/timeline", "127.0.0.1:9999", nil, "", 403},
		{"foreign host on the API", "GET", "/api/notes", "evil.example", nil, "", 403},
		{"same origin", "POST", "/api/notes", good, map[string]string{"Origin": "http://127.0.0.1:" + testPort, "Content-Type": "application/json"}, `{"text":"x"}`, 201},
		{"localhost origin", "POST", "/api/notes", good, map[string]string{"Origin": "http://localhost:" + testPort, "Content-Type": "application/json"}, `{"text":"x"}`, 201},
		{"foreign origin POST", "POST", "/api/notes", good, map[string]string{"Origin": "https://evil.example", "Content-Type": "application/json"}, `{"text":"x"}`, 403},
		{"foreign origin DELETE", "DELETE", "/api/notes/1", good, map[string]string{"Origin": "https://evil.example"}, "", 403},
		{"foreign origin GET", "GET", "/api/notes", good, map[string]string{"Origin": "https://evil.example"}, "", 403},
		{"null origin", "POST", "/api/notes", good, map[string]string{"Origin": "null", "Content-Type": "application/json"}, `{"text":"x"}`, 403},
		{"text/plain POST", "POST", "/api/notes", good, map[string]string{"Content-Type": "text/plain"}, `{"text":"x"}`, 415},
		{"form POST", "POST", "/api/notes", good, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, `text=x`, 415},
		{"no content type POST", "POST", "/api/notes", good, nil, `{"text":"x"}`, 415},
		{"text/plain PUT", "PUT", "/api/notes/1/topic", good, map[string]string{"Content-Type": "text/plain"}, `{"topic":"x"}`, 415},
		{"text/plain annotation", "POST", "/api/notes/1/annotations", good, map[string]string{"Content-Type": "text/plain"}, `{"kind":"memo","body":"x"}`, 415},
		{"text/plain topics", "POST", "/api/topics", good, map[string]string{"Content-Type": "text/plain"}, `{"name":"x"}`, 415},
		{"json with charset", "POST", "/api/notes", good, jsCharset, `{"text":"x"}`, 201},
		{"json PUT", "PUT", "/api/notes/1/topic", good, js, `{"topic":"x"}`, 200},
		{"DELETE needs no content type", "DELETE", "/api/notes/1", good, nil, "", 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := send(h, tt.method, tt.path, tt.host, tt.hdr, tt.body).Code; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGuardRejectedWritesStoreNothing(t *testing.T) {
	h, store := newPages(t)
	send(h, "POST", "/api/notes", "127.0.0.1:"+testPort, map[string]string{"Content-Type": "text/plain"}, `{"text":"x"}`)
	send(h, "POST", "/api/notes", "evil.example", map[string]string{"Content-Type": "application/json"}, `{"text":"x"}`)
	if got := len(store.List(note.Filter{})); got != 0 {
		t.Errorf("refused requests stored %d memos", got)
	}
}
