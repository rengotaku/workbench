// Package note holds the memo model and the store that keeps it on disk.
package note

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Annotation kinds. A memo gets annotations appended over time; none is ever
// edited or removed.
const (
	KindQuote    = "quote"
	KindAnalysis = "analysis"
	KindMemo     = "memo"
)

// Annotation authors.
const (
	AuthorAI    = "ai"
	AuthorHuman = "human"
)

// Annotation is one appended supplement to a memo.
type Annotation struct {
	CreatedAt time.Time `json:"created_at"`
	Kind      string    `json:"kind"`
	Body      string    `json:"body"`
	Source    string    `json:"source,omitempty"`
	Author    string    `json:"author"`
	ID        int       `json:"id"`
}

// Note is a one-line memo: the text as typed, the first link in it, and the
// annotations added since.
type Note struct {
	CreatedAt   time.Time    `json:"created_at"`
	Text        string       `json:"text"`
	URL         string       `json:"url"`
	Topic       string       `json:"topic"`
	Annotations []Annotation `json:"annotations"`
	ID          int          `json:"id"`
}

// Pending reports whether the memo is waiting for its first annotation.
func (n Note) Pending() bool { return len(n.Annotations) == 0 }

// Filter narrows a list. The zero value matches everything.
type Filter struct {
	Topic   string // only this topic
	NoTopic bool   // only memos without a topic
	Pending bool   // only memos without annotations
}

func (f Filter) match(n Note) bool {
	if f.Topic != "" && n.Topic != f.Topic {
		return false
	}
	if f.NoTopic && n.Topic != "" {
		return false
	}
	return !f.Pending || n.Pending()
}

var urlPattern = regexp.MustCompile(`https?://[A-Za-z0-9\-._~:/?#\[\]@!$&()*+,;=%]+`)

// A link is ASCII only, so Japanese text glued to it ("…/x。次") ends it. These
// trailing characters belong to the sentence around a link, not to it.
const urlTrailing = ".,;:!?)]"

// ExtractURL returns the first http(s) URL in text, or "" when there is none.
func ExtractURL(text string) string {
	m := urlPattern.FindString(text)
	return strings.TrimRight(m, urlTrailing)
}

// sourceHosts maps a host suffix to the label shown for it.
var sourceHosts = []struct{ suffix, label string }{
	{"slack.com", "Slack"},
	{"notion.so", "Notion"},
	{"notion.site", "Notion"},
	{"app.notion.com", "Notion"},
	{"backlog.com", "Backlog"},
	{"backlog.jp", "Backlog"},
}

// SourceLabel names where a link points, from its host alone. It is derived
// on display and never stored. An empty url gives "".
func SourceLabel(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "リンク"
	}
	host := strings.ToLower(u.Hostname())
	for _, s := range sourceHosts {
		if host == s.suffix || strings.HasSuffix(host, "."+s.suffix) {
			return s.label
		}
	}
	return "リンク"
}

// Segment is a run of memo text: plain, or (URL set) a link.
type Segment struct {
	Text string
	URL  string // non-empty for a link; equals Text
}

// SplitLinks cuts text into plain runs and http(s) links, in order, so a page
// can link the URLs without treating the rest as markup.
func SplitLinks(text string) []Segment {
	var out []Segment
	rest := text
	for rest != "" {
		loc := urlPattern.FindStringIndex(rest)
		if loc == nil {
			out = append(out, Segment{Text: rest})
			break
		}
		link := strings.TrimRight(rest[loc[0]:loc[1]], urlTrailing)
		if loc[0] > 0 {
			out = append(out, Segment{Text: rest[:loc[0]]})
		}
		out = append(out, Segment{Text: link, URL: link})
		rest = rest[loc[0]+len(link):]
	}
	return out
}
