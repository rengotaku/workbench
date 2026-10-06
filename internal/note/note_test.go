package note

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExtractURL(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"none", "ただのメモ", ""},
		{"leading link", "https://fundinno-inc.slack.com/archives/C1/p1 staging が変わった", "https://fundinno-inc.slack.com/archives/C1/p1"},
		{"link in the middle", "見て http://example.com/a?b=1 これ", "http://example.com/a?b=1"},
		{"first of two", "https://a.example/1 https://b.example/2", "https://a.example/1"},
		{"trailing punctuation", "(https://example.com/x). です", "https://example.com/x"},
		{"japanese text glued on", "https://example.com/x。次の話", "https://example.com/x"},
		{"not http", "ftp://example.com/x", ""},
		{"brackets inside the link", "https://en.wikipedia.org/wiki/Go_(programming_language) 見て", "https://en.wikipedia.org/wiki/Go_(programming_language)"},
		{"link with brackets inside brackets", "(https://en.wikipedia.org/wiki/Go_(programming_language))", "https://en.wikipedia.org/wiki/Go_(programming_language)"},
		{"square brackets inside the link", "https://example.com/a?x[0]=1 です", "https://example.com/a?x[0]=1"},
		{"apostrophe inside the link", "https://example.com/it's/here 見て", "https://example.com/it's/here"},
		{"quoted link", "'https://example.com/x'", "https://example.com/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractURL(tt.text); got != tt.want {
				t.Errorf("ExtractURL(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestSourceLabel(t *testing.T) {
	tests := []struct{ url, want string }{
		{"", ""},
		{"https://fundinno-inc.slack.com/archives/C1/p1", "Slack"},
		{"https://slack.com/x", "Slack"},
		{"https://www.notion.so/page", "Notion"},
		{"https://team.notion.site/page", "Notion"},
		{"https://app.notion.com/p/abc", "Notion"},
		{"https://space.backlog.com/view/DSC-1", "Backlog"},
		{"https://space.backlog.jp/view/DSC-1", "Backlog"},
		{"https://example.com/", "リンク"},
		{"https://evilslack.com/", "リンク"},
		{"https://slack.com.evil.example/", "リンク"},
		{"http://[bad", "リンク"},
	}
	for _, tt := range tests {
		if got := SourceLabel(tt.url); got != tt.want {
			t.Errorf("SourceLabel(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestExtractURLLongClosingRun(t *testing.T) {
	text := "https://example.com/x" + strings.Repeat(")", 200000)
	start := time.Now()
	if got := ExtractURL(text); got != "https://example.com/x" {
		t.Fatalf("ExtractURL = %q", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("trimming 200000 closers took %v; it should be linear", d)
	}
}

func TestSplitLinks(t *testing.T) {
	got := SplitLinks("前 https://a.example/x). 後 https://b.example/y")
	want := []Segment{
		{Text: "前 "},
		{Text: "https://a.example/x", URL: "https://a.example/x"},
		{Text: "). 後 "},
		{Text: "https://b.example/y", URL: "https://b.example/y"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitLinks = %#v, want %#v", got, want)
	}
	got = SplitLinks("https://en.wikipedia.org/wiki/Go_(programming_language) 見て")
	if len(got) != 2 || got[0].URL != "https://en.wikipedia.org/wiki/Go_(programming_language)" {
		t.Errorf("a bracket opened inside the link should stay in it, got %#v", got)
	}
	if got := SplitLinks("リンクなし"); len(got) != 1 || got[0].URL != "" {
		t.Errorf("plain text should be one plain segment, got %#v", got)
	}
	if got := SplitLinks(""); len(got) != 0 {
		t.Errorf("empty text should give no segments, got %#v", got)
	}
}

func TestFilterMatch(t *testing.T) {
	annotated := Note{ID: 1, Topic: "A", Annotations: []Annotation{{ID: 1}}}
	bare := Note{ID: 2}
	if !(Filter{}).match(annotated) || !(Filter{}).match(bare) {
		t.Error("zero filter should match everything")
	}
	if (Filter{Pending: true}).match(annotated) {
		t.Error("pending filter should drop annotated memos")
	}
	if !(Filter{NoTopic: true}).match(bare) || (Filter{NoTopic: true}).match(annotated) {
		t.Error("no-topic filter mismatch")
	}
	if (Filter{Topic: "B"}).match(annotated) {
		t.Error("topic filter should drop other topics")
	}
}
