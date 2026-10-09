package note

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseLinkInput(t *testing.T) {
	tests := []struct {
		in   string
		want LinkInput
	}{
		{"https://x.slack.com/a/1", LinkInput{URL: "https://x.slack.com/a/1"}},
		{"  https://x.slack.com/a/1   スレッド 本体 ", LinkInput{URL: "https://x.slack.com/a/1", Label: "スレッド 本体"}},
		{"https://x.notion.so/p　議事録", LinkInput{URL: "https://x.notion.so/p", Label: "議事録"}},
		{"", LinkInput{}},
	}
	for _, tt := range tests {
		if got := ParseLinkInput(tt.in); got != tt.want {
			t.Errorf("ParseLinkInput(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestSplitMemo(t *testing.T) {
	got := SplitMemo("[1] と https://example.com/a?x[2]=1 と[2]、[0] [12345] ［3］")
	want := []Segment{
		{Text: "[1]", Ref: 1},
		{Text: " と "},
		{Text: "https://example.com/a?x[2]=1", URL: "https://example.com/a?x[2]=1"},
		{Text: " と"},
		{Text: "[2]", Ref: 2},
		{Text: "、[0] [12345] ［3］"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitMemo =\n %+v\nwant\n %+v", got, want)
	}
	if got := SplitMemo(""); got != nil {
		t.Errorf("SplitMemo(\"\") = %+v", got)
	}
}

func TestAddWithLinks(t *testing.T) {
	s, dir := openStore(t)
	n, err := s.Add("[1] の件は [2] で決まった", "", LinkInput{URL: " https://x.slack.com/a/1 ", Label: " スレッド "}, LinkInput{URL: "http://space.backlog.com/view/DSC-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Link{{N: 1, URL: "https://x.slack.com/a/1", Label: "スレッド"}, {N: 2, URL: "http://space.backlog.com/view/DSC-1"}}
	if !reflect.DeepEqual(n.Links, want) {
		t.Errorf("Links = %+v, want %+v", n.Links, want)
	}
	if n.URL != "" {
		t.Errorf("URL is the first link in the text, not an attached one: %q", n.URL)
	}

	if closeErr := s.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	again, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Get(n.ID); !reflect.DeepEqual(got.Links, want) {
		t.Errorf("Links after reopen = %+v", got.Links)
	}
}

func TestAddWithoutLinksStoresNoLinksKey(t *testing.T) {
	s, dir := openStore(t)
	mustAdd(t, s, "脚注 [1] だけのメモ", "") // no links: [1] is plain text
	b, err := os.ReadFile(filepath.Join(dir, "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"links"`) {
		t.Errorf("a memo without links should be stored as before: %s", b)
	}
}

func TestOpenReadsMemosFromBeforeLinks(t *testing.T) {
	dir := t.TempDir()
	old := `{"notes":[{"id":1,"text":"a","url":"","topic":"","annotations":[],"created_at":"2026-10-06T00:00:00Z"}],"topics":[],"next_note":2,"next_annotation":1}`
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, clock())
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Get(1)
	if err != nil || n.Links != nil {
		t.Errorf("Get = %+v, %v", n, err)
	}
	raw, _ := json.Marshal(n)
	if strings.Contains(string(raw), `"links"`) {
		t.Errorf("JSON of an old memo should have no links key: %s", raw)
	}
}

func TestAddRejectsBadLinks(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		links []LinkInput
	}{
		{"empty url", "a", "関連 URL [1]", []LinkInput{{URL: " "}}},
		{"not http", "a", "関連 URL [2]", []LinkInput{{URL: "https://ok.example/"}, {URL: "ftp://x.example/"}}},
		{"no host", "a", "関連 URL [1]", []LinkInput{{URL: "https:///path"}}},
		{"relative", "a", "関連 URL [1]", []LinkInput{{URL: "/timeline"}}},
		{"unparsable", "a", "関連 URL [1]", []LinkInput{{URL: "http://[bad"}}},
		{"ref beyond links", "[1] と [3]", "[3] に当たる関連 URL がありません（関連 URL は 2 件）", []LinkInput{{URL: "https://a.example/"}, {URL: "https://b.example/"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := openStore(t)
			_, err := s.Add(tt.text, "", tt.links...)
			var invalid InvalidError
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want InvalidError containing %q", err, tt.want)
			}
			if len(s.List(Filter{})) != 0 {
				t.Error("a rejected memo must not be stored")
			}
		})
	}
}

func TestAddAllowsUnreferencedLinksAndRefsInsideURLs(t *testing.T) {
	s, _ := openStore(t)
	// The [5] belongs to the URL in the text, not to a link reference.
	if _, err := s.Add("https://example.com/a?x[5]=1 見て", "", LinkInput{URL: "https://a.example/"}); err != nil {
		t.Errorf("err = %v", err)
	}
}
