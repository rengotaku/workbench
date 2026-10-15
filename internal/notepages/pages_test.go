package notepages

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/httpguard"
	"github.com/rengotaku/workbench/internal/note"
	noteweb "github.com/rengotaku/workbench/web/note"
)

const testPort = "8085"

// routes mounts p the way cmd/server does: behind the guard, on one engine.
func routes(p *Pages) http.Handler {
	r := gin.New()
	r.Use(httpguard.New(testPort))
	p.Mount(r)
	return r
}

func newPages(t *testing.T) (http.Handler, *note.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := LoadTemplates(noteweb.FS)
	if err != nil {
		t.Fatal(err)
	}
	static, err := fs.Sub(noteweb.FS, "static")
	if err != nil {
		t.Fatal(err)
	}
	return routes(New(templates, static, store)), store
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

	code, body := get(t, h, "/notes")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	mustContain(t, body, `id="memo"`, "autofocus", `data-topic="青分離"`, `data-topic="C&amp;C"`, "＋ 話題を追加", "/notes/static/js/add.js")
	mustNotContain(t, body, "https://fonts.", "cdn.")
}

func TestTimelineGroupsByDayNewestFirst(t *testing.T) {
	h, store := newPages(t)
	// The store stamps notes with the real clock, so group by the dates it produced.
	_, _ = store.Add("古いメモ", "")
	_, _ = store.Add("新しいメモ", "")

	_, body := get(t, h, "/notes/timeline")
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
	triaged := time.Date(2026, 10, 2, 10, 20, 0, 0, loc)
	notes := []note.Note{
		{ID: 3, Text: "c", CreatedAt: time.Date(2026, 10, 6, 16, 45, 0, 0, loc)},
		{ID: 2, Text: "b", CreatedAt: time.Date(2026, 10, 6, 11, 20, 0, 0, loc)},
		{ID: 1, Text: "a", CreatedAt: time.Date(2026, 10, 2, 10, 5, 0, 0, loc), TriagedAt: &triaged, Route: &note.Route{To: note.RouteTodo, TaskID: "T004"}, Annotations: []note.Annotation{{ID: 1, Kind: note.KindMemo, Author: note.AuthorAI, Body: "x", CreatedAt: time.Date(2026, 10, 2, 10, 20, 0, 0, loc)}}},
	}
	v := buildTimeline(notes, nil, "", false)
	if len(v.Days) != 2 || len(v.Days[0].Notes) != 2 || len(v.Days[1].Notes) != 1 {
		t.Fatalf("days = %+v", v.Days)
	}
	if v.Pending != 2 {
		t.Errorf("Pending = %d, want 2", v.Pending)
	}
	if r := v.Days[1].Notes[0].Route; r == nil || r.Label != "→ todo T004" || r.Href != "/" {
		t.Errorf("route view = %+v", r)
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
	_, _ = store.SetRoute(n.ID, note.Route{To: note.RouteTodo, TaskID: "T007"})
	rec, _ := store.Add("記録にしたメモ", "")
	_, _ = store.SetRoute(rec.ID, note.Route{To: note.RouteRecord})

	_, body := get(t, h, "/notes/timeline")
	mustContain(t, body,
		`<a href="https://fundinno-inc.slack.com/archives/C1/p1" target="_blank" rel="noopener noreferrer">`,
		`<span class="src">Slack</span>`,
		`<blockquote class="body">剣持: 変更します`,
		"引用 · AI", "考察 · 人", " · slack",
		"補足待ち 1",
		`<div class="pending">補足待ち</div>`,
		`<a class="route" href="/">→ todo T007</a>`,
		`<span class="route">記録</span>`,
	)
}

func TestTimelineEscapesUserContent(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add(`<script>alert(1)</script> https://example.com/?a=1&b="2"`, `<b>話題</b>`)
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindAnalysis, Body: `<img src=x onerror=alert(2)>`})

	_, body := get(t, h, "/notes/timeline")
	mustNotContain(t, body, "<script>alert(1)", "<img src=x", "<b>話題</b>")
	mustContain(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;img src=x onerror=alert(2)&gt;")
}

func TestTimelineFilters(t *testing.T) {
	h, store := newPages(t)
	_, _ = store.Add("青のメモ", "青分離")
	_, _ = store.Add("CCのメモ", "C&C")
	_, _ = store.Add("無印のメモ", "")

	_, all := get(t, h, "/notes/timeline")
	mustContain(t, all, "青のメモ", "CCのメモ", "無印のメモ", `href="/notes/timeline?topic=C%26C"`, `href="/notes/timeline?no_topic=1"`)
	mustContain(t, all, `class="chip on" href="/notes/timeline" aria-current="page"`)

	_, blue := get(t, h, "/notes/timeline?topic=%E9%9D%92%E5%88%86%E9%9B%A2")
	mustContain(t, blue, "青のメモ", `class="chip on" href="/notes/timeline?topic=`)
	mustNotContain(t, blue, "CCのメモ", "無印のメモ")

	_, none := get(t, h, "/notes/timeline?no_topic=1")
	mustContain(t, none, "無印のメモ")
	mustNotContain(t, none, "青のメモ", "CCのメモ")

	_, nobody := get(t, h, "/notes/timeline?topic=nope")
	mustContain(t, nobody, "この絞り込みに合うメモはありません")
}

func TestTimelineEmpty(t *testing.T) {
	h, _ := newPages(t)
	_, body := get(t, h, "/notes/timeline")
	mustContain(t, body, "まだメモがありません", "補足待ち 0")
}

func TestStatic(t *testing.T) {
	h, _ := newPages(t)
	for _, p := range []string{"/notes/static/css/style.css", "/notes/static/js/shared.js", "/notes/static/js/add.js", "/notes/static/js/timeline.js"} {
		if code, body := get(t, h, p); code != http.StatusOK || body == "" {
			t.Errorf("%s: status %d, %d bytes", p, code, len(body))
		}
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
	h := routes(New(map[string]*template.Template{}, fstest.MapFS{}, store))
	if code, _ := get(t, h, "/notes"); code != http.StatusInternalServerError {
		t.Errorf("missing template status = %d", code)
	}

	// A template that fails while executing ends in an error text, not a panic.
	broken := template.Must(template.New("base").Parse(`{{.NoSuchField}}`))
	h = routes(New(map[string]*template.Template{"add.html": broken}, fstest.MapFS{}, store))
	if _, body := get(t, h, "/notes"); !strings.Contains(body, "画面を描画できませんでした") {
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
		{"loopback host", "GET", "/notes/timeline", good, nil, "", 200},
		{"localhost host", "GET", "/notes/timeline", "localhost:" + testPort, nil, "", 200},
		{"foreign host (DNS rebinding)", "GET", "/notes/timeline", "evil.example:" + testPort, nil, "", 403},
		{"wrong port", "GET", "/notes/timeline", "127.0.0.1:9999", nil, "", 403},
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

func TestTimelineShowsLinksAndReferences(t *testing.T) {
	h, store := newPages(t)
	_, err := store.Add("[1] で決定、[2] は無関係の脚注", "",
		note.LinkInput{URL: "https://x.slack.com/a/1", Label: "決定スレッド"})
	if err == nil {
		t.Fatal("[2] without a second link should be rejected")
	}
	if _, err := store.Add("[1] で決定、詳細は [2]", "",
		note.LinkInput{URL: "https://x.slack.com/a/1", Label: "決定スレッド"},
		note.LinkInput{URL: "https://space.backlog.com/view/DSC-1"}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.Add("脚注 [1] だけ", "")

	_, body := get(t, h, "/notes/timeline")
	mustContain(t, body,
		`<a class="ref" href="https://x.slack.com/a/1" target="_blank" rel="noopener noreferrer" title="https://x.slack.com/a/1">[1]</a>`,
		`<a class="ref" href="https://space.backlog.com/view/DSC-1"`,
		`<span class="ln">[1]</span> <span class="src">Slack</span> <a href="https://x.slack.com/a/1" target="_blank" rel="noopener noreferrer">決定スレッド</a>`,
		`<span class="ln">[2]</span> <span class="src">Backlog</span> <a href="https://space.backlog.com/view/DSC-1" target="_blank" rel="noopener noreferrer">https://space.backlog.com/view/DSC-1</a>`,
		"脚注 [1] だけ",
	)
	if got := strings.Count(body, `class="links"`); got != 1 {
		t.Errorf("only the memo with links should list them, got %d lists", got)
	}
}

func TestToNoteViewLeavesUnmatchedRefAsText(t *testing.T) {
	// Stored data is trusted to be checked on add, but a reference with no
	// link must still render as text rather than an empty link.
	n := note.Note{ID: 1, Text: "[1] と [2]", Links: []note.Link{{N: 1, URL: "https://a.example/"}}}
	nv := toNoteView(n, time.Now())
	want := []segmentView{{Text: "[1]", URL: "https://a.example/", Ref: 1}, {Text: " と "}, {Text: "[2]"}}
	if len(nv.Segments) != len(want) {
		t.Fatalf("segments = %+v", nv.Segments)
	}
	for i := range want {
		if nv.Segments[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, nv.Segments[i], want[i])
		}
	}
}

func TestAddPageHasLinkRows(t *testing.T) {
	h, _ := newPages(t)
	_, body := get(t, h, "/notes")
	mustContain(t, body, `id="links"`, `id="link-add"`, "＋ 関連 URL")
}

// seedTopics adds one memo per topic so the topics have a distinct last-used
// time, oldest first.
func seedTopics(t *testing.T, store *note.Store, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := store.Add("memo "+n, n); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSplitTopics(t *testing.T) {
	at := func(m int) *time.Time { v := time.Date(2026, 10, 1, 0, m, 0, 0, time.UTC); return &v }
	var stats []note.TopicStat
	for i, n := range []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"} {
		stats = append(stats, note.TopicStat{Name: n, LastUsed: at(i)})
	}
	stats = append(stats, note.TopicStat{Name: "unused"})

	shown, rest := splitTopics(stats, "")
	if want := []string{"t8", "t7", "t6", "t5", "t4", "t3"}; !reflect.DeepEqual(shown, want) {
		t.Errorf("shown = %v, want %v", shown, want)
	}
	if want := []string{"t2", "t1", "unused"}; !reflect.DeepEqual(rest, want) {
		t.Errorf("rest = %v, want %v", rest, want)
	}

	// The selected topic is shown even when it is not among the six.
	shown, rest = splitTopics(stats, "t1")
	if want := []string{"t8", "t7", "t6", "t5", "t4", "t3", "t1"}; !reflect.DeepEqual(shown, want) {
		t.Errorf("shown with selection = %v, want %v", shown, want)
	}
	if want := []string{"t2", "unused"}; !reflect.DeepEqual(rest, want) {
		t.Errorf("rest with selection = %v, want %v", rest, want)
	}

	// An unknown selection adds nothing.
	if shown, _ = splitTopics(stats[:2], "nope"); len(shown) != 2 {
		t.Errorf("shown = %v", shown)
	}
}

func TestTimelineFoldsOlderTopicsIntoOther(t *testing.T) {
	h, store := newPages(t)
	seedTopics(t, store, "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8")

	_, all := get(t, h, "/notes/timeline")
	mustContain(t, all, `topic=t8"`, `topic=t3"`, `id="chip-more"`, "他 2 ▾", `data-others="[&#34;t2&#34;,&#34;t1&#34;]"`, `id="organize"`, "話題を整理", `id="organize-dialog"`)
	mustNotContain(t, all, `topic=t2"`, `topic=t1"`)
	if strings.Index(all, "topic=t8") > strings.Index(all, "topic=t3") {
		t.Error("most recently used topic should come first")
	}
	if strings.Index(all, "no_topic=1") > strings.Index(all, `id="chip-more"`) {
		t.Error("他 N comes after 話題なし")
	}

	_, sel := get(t, h, "/notes/timeline?topic=t1")
	mustContain(t, sel, `class="chip on" href="/notes/timeline?topic=t1" data-topic="t1"`, "他 1 ▾")
}

func TestTimelineHasNoOtherChipWhenAllShown(t *testing.T) {
	h, store := newPages(t)
	seedTopics(t, store, "a", "b")
	_, body := get(t, h, "/notes/timeline")
	mustContain(t, body, `id="chip-more" hidden`)
}

func TestAddPageFoldsTopics(t *testing.T) {
	h, store := newPages(t)
	seedTopics(t, store, "t1", "t2", "t3", "t4", "t5", "t6", "t7")
	_, body := get(t, h, "/notes")
	mustContain(t, body, `data-topic="t7"`, `data-topic="t2"`, "他 1 ▾", `id="topic-more"`, `id="pending-title"`, "AI が拾うと一覧から消える", "/notes/static/js/picker.js")
	mustNotContain(t, body, `data-topic="t1"`, "元に戻す")
	mustContain(t, body, "&#34;t1&#34;") // every topic is still in the data for the input
}

func TestAddPageHidesOtherChipWhenNothingFolded(t *testing.T) {
	h, store := newPages(t)
	seedTopics(t, store, "a")
	_, body := get(t, h, "/notes")
	mustContain(t, body, `id="topic-more" hidden`)
}

// TestTimelineJSONMatchesPage checks that the JSON a script draws from lists
// the same memos, in the same order, as the page for every filter.
func TestTimelineJSONMatchesPage(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add("青のメモ https://example.com/a [1]", "青分離")
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindQuote, Body: "引用の本文", Source: "slack"})
	_, _ = store.Add("CCのメモ", "C&C")
	_, _ = store.Add("無印のメモ", "")

	idRe := regexp.MustCompile(`id="note-(\d+)"`)
	for _, q := range []string{"", "?topic=" + url.QueryEscape("青分離"), "?no_topic=1", "?topic=nope"} {
		_, page := get(t, h, "/notes/timeline"+q)
		var want []int
		for _, m := range idRe.FindAllStringSubmatch(page, -1) {
			id, _ := strconv.Atoi(m[1])
			want = append(want, id)
		}

		code, body := get(t, h, "/api/notes/timeline"+q)
		if code != http.StatusOK {
			t.Fatalf("%q: status = %d", q, code)
		}
		var got timelineJSON
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		var ids []int
		for _, d := range got.Days {
			for _, nv := range d.Notes {
				ids = append(ids, nv.ID)
			}
		}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("%q: JSON ids = %v, page ids = %v", q, ids, want)
		}
		if !strings.Contains(page, got.Empty) && len(want) == 0 {
			t.Errorf("%q: empty message %q not on the page", q, got.Empty)
		}
		if len(got.Chips) == 0 {
			t.Errorf("%q: no chips", q)
		}
	}
}

func TestTimelineJSONShape(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add("メモ https://example.com/a", "青分離")
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindAnalysis, Body: "考察", Author: note.AuthorHuman})
	_, _ = store.Add("補足なし", "")

	_, body := get(t, h, "/api/notes/timeline")
	mustContain(t, body, `"pending":2`, `"label":"考察"`, `"author":"人"`, `"other":[]`, `"links":[]`, `"annotations":[]`)
	mustNotContain(t, body, "null")

	// The static segment must not swallow /api/notes/:id.
	code, one := get(t, h, "/api/notes/"+strconv.Itoa(n.ID))
	if code != http.StatusOK || !strings.Contains(one, "メモ") {
		t.Errorf("/api/notes/%d: %d %s", n.ID, code, one)
	}
}
