// Package notepages serves the two screens (add at /notes, timeline at
// /notes/timeline) and the static bundle, and mounts the API beside them.
package notepages

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteapi"
)

// Pages renders the screens from the store.
type Pages struct {
	templates map[string]*template.Template
	staticFS  fs.FS
	store     *note.Store
	api       *noteapi.API
}

// LoadTemplates builds a per-page template set so each page gets its own
// "content" definition. A single shared set lets the last-parsed file's
// {{define "content"}} win for every page.
func LoadTemplates(fsys fs.FS) (map[string]*template.Template, error) {
	m := make(map[string]*template.Template)
	for _, name := range []string{"add.html", "timeline.html"} {
		t, err := template.ParseFS(fsys, "templates/base.html", "templates/"+name)
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

// ---- add screen ----

type addView struct {
	Title  string
	Topics []string
}

func (p *Pages) handleAdd(c *gin.Context) {
	p.render(c, "add.html", addView{Title: "note", Topics: p.store.Topics()})
}

// ---- timeline ----

type chipView struct {
	Label string
	Href  string
	On    bool
}

// segmentView is a run of memo text. A reference ([N]) to a related link
// is shown as a link to that URL; one with no such link stays plain text.
type segmentView struct {
	Text string
	URL  string
	Ref  int
}

type linkView struct {
	URL    string
	Label  string
	Source string
	N      int
}

type annotationView struct {
	Kind   string // quote | analysis | memo
	Label  string // 引用 | 考察 | メモ
	Author string
	When   string
	Source string
	Body   string
}

// routeView says where the AI sent a memo once it picked it up.
type routeView struct {
	Label string // → todo | 記録
	Href  string // the todo list for a task, empty for a record
}

type noteView struct {
	Route       *routeView
	Time        string
	Source      string
	Topic       string
	Segments    []segmentView
	Links       []linkView
	Annotations []annotationView
	ID          int
	Pending     bool // the AI has not picked it up yet
}

type dayView struct {
	Heading string
	Notes   []noteView
}

type timelineView struct {
	Title   string
	Empty   string
	Chips   []chipView
	Days    []dayView
	Pending int
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
	p.render(c, "timeline.html", buildTimeline(notes, p.store.Topics(), topic, noTopic))
}

func buildTimeline(notes []note.Note, topics []string, topic string, noTopic bool) timelineView {
	chips := []chipView{{Label: "すべて", Href: TimelinePath, On: topic == "" && !noTopic}}
	for _, t := range topics {
		chips = append(chips, chipView{Label: t, Href: TimelinePath + "?topic=" + url.QueryEscape(t), On: t == topic && !noTopic})
	}
	chips = append(chips, chipView{Label: "話題なし", Href: TimelinePath + "?no_topic=1", On: noTopic})

	v := timelineView{Title: "note タイムライン", Chips: chips, Empty: "まだメモがありません。note と打って 1 行入れてください"}
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
