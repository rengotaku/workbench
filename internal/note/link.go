package note

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Link is a related URL attached to a memo when it is added. N numbers the
// links of one memo from 1, so the memo text can point at one as [N]. Links
// are fixed with the memo and never renumbered.
type Link struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
	N     int    `json:"n"`
}

// LinkInput is a link as a caller supplies it; the store assigns N.
type LinkInput struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// ParseLinkInput reads "URL 一言": the first word is the URL, the rest (if
// any) its label. The CLI and the add screen take a link in this form.
func ParseLinkInput(s string) LinkInput {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, isSpace)
	if i < 0 {
		return LinkInput{URL: s}
	}
	return LinkInput{URL: s[:i], Label: strings.TrimSpace(s[i:])}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '　' }

// buildLinks validates the inputs and numbers them in order.
func buildLinks(in []LinkInput) ([]Link, error) {
	links := make([]Link, 0, len(in))
	for i, l := range in {
		raw := strings.TrimSpace(l.URL)
		u, err := url.Parse(raw)
		if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, InvalidError(fmt.Sprintf("関連 URL [%d] は http(s) の URL にしてください（受け取った値: %q）", i+1, raw))
		}
		links = append(links, Link{N: i + 1, URL: raw, Label: strings.TrimSpace(l.Label)})
	}
	return links, nil
}

// checkRefs rejects a [N] in text that no link answers to. A memo without
// links is not checked: there "[1]" is just text someone pasted.
func checkRefs(text string, links []Link) error {
	if len(links) == 0 {
		return nil
	}
	for _, s := range SplitMemo(text) {
		if s.Ref > len(links) {
			return InvalidError(fmt.Sprintf("本文の [%d] に当たる関連 URL がありません（関連 URL は %d 件）。番号を直すか、関連 URL を足してください", s.Ref, len(links)))
		}
	}
	return nil
}

// refPattern is a reference to a link: [N] with N from 1, ASCII brackets only.
var refPattern = regexp.MustCompile(`\[([1-9][0-9]{0,3})\]`)

// SplitMemo cuts memo text into plain runs, http(s) links and [N]
// references, in order. A [N] inside a URL stays part of the URL.
func SplitMemo(text string) []Segment {
	var out []Segment
	for _, s := range SplitLinks(text) {
		if s.URL != "" {
			out = append(out, s)
			continue
		}
		out = append(out, splitRefs(s.Text)...)
	}
	return out
}

func splitRefs(text string) []Segment {
	var out []Segment
	rest := text
	for rest != "" {
		loc := refPattern.FindStringSubmatchIndex(rest)
		if loc == nil {
			out = append(out, Segment{Text: rest})
			break
		}
		if loc[0] > 0 {
			out = append(out, Segment{Text: rest[:loc[0]]})
		}
		n, _ := strconv.Atoi(rest[loc[2]:loc[3]]) // at most 4 digits, always parses
		out = append(out, Segment{Text: rest[loc[0]:loc[1]], Ref: n})
		rest = rest[loc[1]:]
	}
	return out
}
