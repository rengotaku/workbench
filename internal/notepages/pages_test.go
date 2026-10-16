package notepages

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
	// The screen draws itself (add.js) from /api/topics and /api/notes; the page
	// carries its root and its scripts.
	mustContain(t, body, `data-view="note-add"`, "/notes/static/js/add.js", "/notes/static/js/picker.js", "/notes/static/css/style.css")
	mustNotContain(t, body, "https://fonts.", "cdn.")
}

// timeline fetches the timeline API (the screen draws from it) and decodes it.
func timeline(t *testing.T, h http.Handler, query string) timelineJSON {
	t.Helper()
	code, body := get(t, h, "/api/notes/timeline"+query)
	if code != http.StatusOK {
		t.Fatalf("%q: status = %d", query, code)
	}
	var got timelineJSON
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	return got
}

// noteIDs is the ids in the order the screen shows them.
func noteIDs(tl timelineJSON) []int {
	var ids []int
	for _, d := range tl.Days {
		for _, nv := range d.Notes {
			ids = append(ids, nv.ID)
		}
	}
	return ids
}

// findNote returns the note with id in tl.
func findNote(t *testing.T, tl timelineJSON, id int) noteView {
	t.Helper()
	for _, d := range tl.Days {
		for _, nv := range d.Notes {
			if nv.ID == id {
				return nv
			}
		}
	}
	t.Fatalf("note %d not in timeline", id)
	return noteView{}
}

// chipOn is the href of the chip that is on, or "" when none is.
func chipOn(tl timelineJSON) string {
	for _, c := range tl.Chips {
		if c.On {
			return c.Href
		}
	}
	return ""
}

func hasChip(tl timelineJSON, href string) bool {
	for _, c := range tl.Chips {
		if c.Href == href {
			return true
		}
	}
	return false
}

func TestTimelinePage(t *testing.T) {
	h, _ := newPages(t)
	code, body := get(t, h, "/notes/timeline")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// The screen draws itself (timeline.js) from /api/notes/timeline.
	mustContain(t, body, `data-view="timeline"`)
}

func TestTimelineGroupsByDayNewestFirst(t *testing.T) {
	h, store := newPages(t)
	// The store stamps notes with the real clock, so group by the dates it produced.
	old, _ := store.Add("古いメモ", "")
	newer, _ := store.Add("新しいメモ", "")

	tl := timeline(t, h, "")
	if want := []int{newer.ID, old.ID}; !reflect.DeepEqual(noteIDs(tl), want) {
		t.Errorf("ids = %v, want newest first %v", noteIDs(tl), want)
	}
	if len(tl.Days) != 1 {
		t.Fatalf("both memos were added today, want one day heading, got %d", len(tl.Days))
	}
	if want := time.Now().Format("1/2") + "（"; !strings.HasPrefix(tl.Days[0].Heading, want) {
		t.Errorf("heading = %q, want prefix %q", tl.Days[0].Heading, want)
	}
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
	bare, _ := store.Add("補足のないメモ", "")
	_, _ = store.SetRoute(n.ID, note.Route{To: note.RouteTodo, TaskID: "T007"})
	rec, _ := store.Add("記録にしたメモ", "")
	_, _ = store.SetRoute(rec.ID, note.Route{To: note.RouteRecord})

	tl := timeline(t, h, "")
	if tl.Pending != 1 {
		t.Errorf("pending = %d, want 1", tl.Pending)
	}
	got := findNote(t, tl, n.ID)
	if got.Source != "Slack" {
		t.Errorf("source = %q", got.Source)
	}
	if len(got.Segments) == 0 || got.Segments[0].URL != "https://fundinno-inc.slack.com/archives/C1/p1" {
		t.Errorf("segments = %+v", got.Segments)
	}
	if got.Route == nil || got.Route.Label != "→ todo T007" || got.Route.Href != "/" {
		t.Errorf("route = %+v", got.Route)
	}
	if len(got.Annotations) != 2 {
		t.Fatalf("annotations = %+v", got.Annotations)
	}
	q, a := got.Annotations[0], got.Annotations[1]
	if q.Label != "引用" || q.Author != "AI" || q.Source != "slack" || !strings.HasPrefix(q.Body, "剣持: 変更します") {
		t.Errorf("quote = %+v", q)
	}
	if a.Label != "考察" || a.Author != "人" || a.Body != "考察の本文" {
		t.Errorf("analysis = %+v", a)
	}
	if b := findNote(t, tl, bare.ID); !b.Pending || b.Route != nil {
		t.Errorf("bare memo = %+v", b)
	}
	if r := findNote(t, tl, rec.ID).Route; r == nil || r.Label != "記録" || r.Href != "" {
		t.Errorf("record route = %+v", r)
	}
}

// HTML escaping happens in the client (the script draws with textContent), so
// the API must hand user text back verbatim.
func TestTimelineReturnsUserContentVerbatim(t *testing.T) {
	h, store := newPages(t)
	n, _ := store.Add(`<script>alert(1)</script> https://example.com/?a=1&b="2"`, `<b>話題</b>`)
	_, _ = store.Annotate(n.ID, note.AnnotationInput{Kind: note.KindAnalysis, Body: `<img src=x onerror=alert(2)>`})

	got := findNote(t, timeline(t, h, ""), n.ID)
	var text strings.Builder
	for _, s := range got.Segments {
		text.WriteString(s.Text)
	}
	if want := `<script>alert(1)</script> https://example.com/?a=1&b="2"`; text.String() != want {
		t.Errorf("segments text = %q, want %q", text.String(), want)
	}
	if got.Topic != `<b>話題</b>` {
		t.Errorf("topic = %q", got.Topic)
	}
	if len(got.Annotations) != 1 || got.Annotations[0].Body != `<img src=x onerror=alert(2)>` {
		t.Errorf("annotations = %+v", got.Annotations)
	}
}

func TestTimelineFilters(t *testing.T) {
	h, store := newPages(t)
	blue, _ := store.Add("青のメモ", "青分離")
	_, _ = store.Add("CCのメモ", "C&C")
	none, _ := store.Add("無印のメモ", "")

	all := timeline(t, h, "")
	if got := noteIDs(all); len(got) != 3 {
		t.Errorf("all ids = %v", got)
	}
	if !hasChip(all, "/notes/timeline?topic=C%26C") || !hasChip(all, "/notes/timeline?no_topic=1") {
		t.Errorf("chips = %+v", all.Chips)
	}
	if chipOn(all) != "/notes/timeline" {
		t.Errorf("すべて should be on, chips = %+v", all.Chips)
	}

	b := timeline(t, h, "?topic="+url.QueryEscape("青分離"))
	if want := []int{blue.ID}; !reflect.DeepEqual(noteIDs(b), want) {
		t.Errorf("blue ids = %v, want %v", noteIDs(b), want)
	}
	if want := "/notes/timeline?topic=" + url.QueryEscape("青分離"); chipOn(b) != want {
		t.Errorf("on chip = %q, want %q", chipOn(b), want)
	}

	n := timeline(t, h, "?no_topic=1")
	if want := []int{none.ID}; !reflect.DeepEqual(noteIDs(n), want) {
		t.Errorf("no_topic ids = %v, want %v", noteIDs(n), want)
	}
	if chipOn(n) != "/notes/timeline?no_topic=1" {
		t.Errorf("話題なし should be on, got %q", chipOn(n))
	}

	nobody := timeline(t, h, "?topic=nope")
	if len(noteIDs(nobody)) != 0 || !strings.Contains(nobody.Empty, "この絞り込みに合うメモはありません") {
		t.Errorf("nobody = %+v", nobody)
	}
}

func TestTimelineEmpty(t *testing.T) {
	h, _ := newPages(t)
	tl := timeline(t, h, "")
	if !strings.Contains(tl.Empty, "まだメモがありません") || tl.Pending != 0 || len(tl.Days) != 0 {
		t.Errorf("timeline = %+v", tl)
	}
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
	linked, err := store.Add("[1] で決定、詳細は [2]", "",
		note.LinkInput{URL: "https://x.slack.com/a/1", Label: "決定スレッド"},
		note.LinkInput{URL: "https://space.backlog.com/view/DSC-1"})
	if err != nil {
		t.Fatal(err)
	}
	foot, _ := store.Add("脚注 [1] だけ", "")

	tl := timeline(t, h, "")
	got := findNote(t, tl, linked.ID)
	wantRefs := []segmentView{{Text: "[1]", URL: "https://x.slack.com/a/1", Ref: 1}, {Text: " で決定、詳細は "}, {Text: "[2]", URL: "https://space.backlog.com/view/DSC-1", Ref: 2}}
	if !reflect.DeepEqual(got.Segments, wantRefs) {
		t.Errorf("segments = %+v, want %+v", got.Segments, wantRefs)
	}
	wantLinks := []linkView{
		{N: 1, URL: "https://x.slack.com/a/1", Label: "決定スレッド", Source: "Slack"},
		{N: 2, URL: "https://space.backlog.com/view/DSC-1", Source: "Backlog"},
	}
	if !reflect.DeepEqual(got.Links, wantLinks) {
		t.Errorf("links = %+v, want %+v", got.Links, wantLinks)
	}
	// A reference with no link stays plain text, and only the memo with links lists them.
	f := findNote(t, tl, foot.ID)
	if len(f.Links) != 0 || len(f.Segments) == 0 {
		t.Errorf("footnote memo = %+v", f)
	}
	for _, s := range f.Segments {
		if s.URL != "" {
			t.Errorf("unlinked reference became a link: %+v", s)
		}
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

	all := timeline(t, h, "")
	var labels []string
	for _, c := range all.Chips {
		labels = append(labels, c.Label)
	}
	// Most recently used first, すべて before and 話題なし after the topics.
	if want := []string{"すべて", "t8", "t7", "t6", "t5", "t4", "t3", "話題なし"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("chips = %v, want %v", labels, want)
	}
	if want := []string{"t2", "t1"}; !reflect.DeepEqual(all.Other, want) {
		t.Errorf("other = %v, want %v", all.Other, want)
	}

	sel := timeline(t, h, "?topic=t1")
	if want := "/notes/timeline?topic=t1"; chipOn(sel) != want {
		t.Errorf("on chip = %q, want %q", chipOn(sel), want)
	}
	if want := []string{"t2"}; !reflect.DeepEqual(sel.Other, want) {
		t.Errorf("other with selection = %v, want %v", sel.Other, want)
	}
}

func TestTimelineHasNoOtherChipWhenAllShown(t *testing.T) {
	h, store := newPages(t)
	seedTopics(t, store, "a", "b")
	if other := timeline(t, h, "").Other; len(other) != 0 {
		t.Errorf("other = %v, want none", other)
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
