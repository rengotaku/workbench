// Package notepages serves the two screens (add at /notes, timeline at
// /notes/timeline) and the static bundle, and mounts the API beside them.
package notepages

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteapi"
	sharedweb "github.com/rengotaku/workbench/web/shared"
)

// Pages renders the screens from the store.
type Pages struct {
	templates map[string]*template.Template
	staticFS  fs.FS
	store     *note.Store
	api       *noteapi.API
}

// LoadTemplates builds a per-page template set (shared layout + tool.html +
// the page) so each page gets its own "content" definition. A single shared set lets the last-parsed file's
// {{define "content"}} win for every page.
func LoadTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	m := make(map[string]*template.Template)
	for _, name := range []string{"add.html", "timeline.html"} {
		t, err := sharedweb.Parse(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("loading template %s: %w", name, err)
		}
		m[name] = t
	}
	return m, nil
}

// Paths of the screens and of the static bundle. They live under /notes so
// they do not collide with the todo screens that share the server.
const (
	AddPath      = "/notes"
	TimelinePath = "/notes/timeline"
	StaticPath   = "/notes/static"

	// TimelineAPIPath returns the timeline as JSON. It sits beside
	// /api/notes/:id; gin prefers the static segment.
	TimelineAPIPath = "/api/notes/timeline"
)

// New wires the screens and the API together.
func New(templates map[string]*template.Template, staticFS fs.FS, store *note.Store) *Pages {
	return &Pages{templates: templates, staticFS: staticFS, store: store, api: noteapi.New(store)}
}

// Mount registers the screens, the static bundle and /api on r. The engine,
// its middleware and /healthz belong to the caller.
func (p *Pages) Mount(r gin.IRouter) {
	r.StaticFS(StaticPath, http.FS(p.staticFS))
	r.GET(AddPath, p.handleAdd)
	r.GET(TimelinePath, p.handleTimeline)
	r.GET(TimelineAPIPath, p.handleTimelineJSON)
	p.api.Mount(r)
}

func (p *Pages) render(c *gin.Context, name string, data any) {
	t, ok := p.templates[name]
	if !ok {
		c.String(http.StatusInternalServerError, "テンプレートが見つかりません: "+name)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := t.ExecuteTemplate(c.Writer, "base", data); err != nil {
		c.String(http.StatusInternalServerError, "画面を描画できませんでした")
	}
}

// visibleTopics is how many topic chips a screen shows before it folds the
// rest into the "他 N" chip.
const visibleTopics = 6

// splitTopics picks the topics to show as chips: the most recently used
// visibleTopics, plus selected when it is not among them. rest is every other
// topic, in the same order.
func splitTopics(stats []note.TopicStat, selected string) (shown, rest []string) {
	for i, t := range note.RankByRecent(stats) {
		if i < visibleTopics || t.Name == selected {
			shown = append(shown, t.Name)
		} else {
			rest = append(rest, t.Name)
		}
	}
	return shown, rest
}

// namesJSON is names as a JSON array, for a data attribute the script reads
// (the template escapes it for the attribute).
func namesJSON(names []string) string {
	b, err := json.Marshal(append([]string{}, names...))
	if err != nil { // a string slice always marshals
		return "[]"
	}
	return string(b)
}

// ---- add screen ----

type addView struct {
	Title     string
	Current   string // which header link is the current screen
	View      string // which screen this is, for the router (router.js)
	AllTopics string // every topic as a JSON array, for the topic input
	Topics    []string
	Others    int // topics folded into "他 N"
}

func (p *Pages) handleAdd(c *gin.Context) {
	stats := p.store.TopicStats()
	shown, rest := splitTopics(stats, "")
	all := make([]string, len(stats))
	for i, t := range stats {
		all[i] = t.Name
	}
	p.render(c, "add.html", addView{Title: "note", Current: "note", View: "note-add", Topics: shown, Others: len(rest), AllTopics: namesJSON(all)})
}

// ---- timeline ----

type chipView struct {
	Label string `json:"label"`
	Href  string `json:"href"`
	Topic string `json:"topic,omitempty"` // the topic a chip filters by; empty for すべて / 話題なし
	On    bool   `json:"on"`
}

// segmentView is a run of memo text. A reference ([N]) to a related link
// is shown as a link to that URL; one with no such link stays plain text.
type segmentView struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	Ref  int    `json:"ref,omitempty"`
}

type linkView struct {
	URL    string `json:"url"`
	Label  string `json:"label"`
	Source string `json:"source"`
	N      int    `json:"n"`
}

type annotationView struct {
	Kind   string `json:"kind"`   // quote | analysis | memo
	Label  string `json:"label"`  // 引用 | 考察 | メモ
	Author string `json:"author"` // AI | 人
	When   string `json:"when"`
	Source string `json:"source,omitempty"`
	Body   string `json:"body"`
}

// routeView says where the AI sent a memo once it picked it up.
type routeView struct {
	Label string `json:"label"`          // → todo | 記録
	Href  string `json:"href,omitempty"` // the todo list for a task, empty for a record
}

type noteView struct {
	Route       *routeView       `json:"route,omitempty"`
	Time        string           `json:"time"`
	Source      string           `json:"source"`
	Topic       string           `json:"topic,omitempty"`
	Segments    []segmentView    `json:"segments"`
	Links       []linkView       `json:"links"`
	Annotations []annotationView `json:"annotations"`
	ID          int              `json:"id"`
	Pending     bool             `json:"pending"` // the AI has not picked it up yet
}

type dayView struct {
	Heading string     `json:"heading"`
	Notes   []noteView `json:"notes"`
}

type timelineView struct {
	Title     string
	Current   string
	View      string
	Empty     string
	Chips     []chipView // すべて, the shown topics, 話題なし
	OtherJSON string     // the topics behind "他 N", as a JSON array
	Days      []dayView
	other     []string // the topics behind "他 N", for the JSON response
	Others    int
	Pending   int
}

// timelineJSON is the timeline screen's content for a script to draw: the
// same grouping, chips and counts the page renders, built by buildTimeline so
// the two cannot disagree.
type timelineJSON struct {
	Empty   string     `json:"empty"`
	Chips   []chipView `json:"chips"`
	Other   []string   `json:"other"` // the topics folded into "他 N"
	Days    []dayView  `json:"days"`
	Pending int        `json:"pending"`
}

func (v timelineView) toJSON() timelineJSON {
	return timelineJSON{
		Empty:   v.Empty,
		Chips:   v.Chips,
		Other:   append([]string{}, v.other...),
		Days:    append([]dayView{}, v.Days...),
		Pending: v.Pending,
	}
}

var kindLabels = map[string]string{
	note.KindQuote:    "引用",
	note.KindAnalysis: "考察",
	note.KindMemo:     "メモ",
}

var weekdays = [...]string{"日", "月", "火", "水", "木", "金", "土"}

func (p *Pages) handleTimeline(c *gin.Context) {
	topic := c.Query("topic")
	noTopic := c.Query("no_topic") == "1"
	notes := p.store.List(note.Filter{Topic: topic, NoTopic: noTopic})
	p.render(c, "timeline.html", buildTimeline(notes, p.store.TopicStats(), topic, noTopic))
}

// handleTimelineJSON takes the same ?topic= / ?no_topic=1 as the page.
func (p *Pages) handleTimelineJSON(c *gin.Context) {
	topic := c.Query("topic")
	noTopic := c.Query("no_topic") == "1"
	notes := p.store.List(note.Filter{Topic: topic, NoTopic: noTopic})
	c.JSON(http.StatusOK, buildTimeline(notes, p.store.TopicStats(), topic, noTopic).toJSON())
}

func buildTimeline(notes []note.Note, stats []note.TopicStat, topic string, noTopic bool) timelineView {
	shown, rest := splitTopics(stats, topic)
	chips := []chipView{{Label: "すべて", Href: TimelinePath, On: topic == "" && !noTopic}}
	for _, t := range shown {
		chips = append(chips, chipView{Label: t, Topic: t, Href: TimelinePath + "?topic=" + url.QueryEscape(t), On: t == topic && !noTopic})
	}
	chips = append(chips, chipView{Label: "話題なし", Href: TimelinePath + "?no_topic=1", On: noTopic})

	v := timelineView{Title: "note タイムライン", Current: "timeline", View: "timeline", Chips: chips, Others: len(rest), OtherJSON: namesJSON(rest), other: rest, Empty: "まだメモがありません。note と打って 1 行入れてください"}
	if topic != "" || noTopic {
		v.Empty = "この絞り込みに合うメモはありません。「すべて」に戻してください"
	}
	for _, n := range notes {
		if n.Pending() {
			v.Pending++
		}
		at := n.CreatedAt.Local()
		heading := at.Format("1/2") + "（" + weekdays[at.Weekday()] + "）"
		if len(v.Days) == 0 || v.Days[len(v.Days)-1].Heading != heading {
			v.Days = append(v.Days, dayView{Heading: heading})
		}
		day := &v.Days[len(v.Days)-1]
		day.Notes = append(day.Notes, toNoteView(n, at))
	}
	return v
}

func toNoteView(n note.Note, at time.Time) noteView {
	nv := noteView{
		ID:     n.ID,
		Time:   at.Format("15:04"),
		Source: note.SourceLabel(n.URL),
		Topic:  n.Topic,
		// Empty, not nil, so the JSON response has [] instead of null.
		Segments:    []segmentView{},
		Links:       []linkView{},
		Annotations: []annotationView{},
		// Pending is the memo's own state, not the absence of annotations.
		Pending: n.Pending(),
	}
	if n.Route != nil {
		nv.Route = &routeView{Label: "記録"}
		if n.Route.To == note.RouteTodo {
			nv.Route = &routeView{Label: "→ todo " + n.Route.TaskID, Href: "/"}
		}
	}
	for _, l := range n.Links {
		nv.Links = append(nv.Links, linkView{N: l.N, URL: l.URL, Label: l.Label, Source: note.SourceLabel(l.URL)})
	}
	for _, s := range note.SplitMemo(n.Text) {
		sv := segmentView{Text: s.Text, URL: s.URL}
		if s.Ref >= 1 && s.Ref <= len(n.Links) {
			sv.URL, sv.Ref = n.Links[s.Ref-1].URL, s.Ref
		}
		nv.Segments = append(nv.Segments, sv)
	}
	for _, a := range n.Annotations {
		nv.Annotations = append(nv.Annotations, annotationView{
			Kind:   a.Kind,
			Label:  kindLabels[a.Kind],
			Author: map[string]string{note.AuthorAI: "AI", note.AuthorHuman: "人"}[a.Author],
			When:   a.CreatedAt.Local().Format("1/2 15:04"),
			Source: a.Source,
			Body:   a.Body,
		})
	}
	return nv
}
