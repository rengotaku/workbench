package task

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Link is a related URL attached to a task. N numbers the links of one task
// from 1, so the task name can point at one as [N]. It has the same shape as
// a memo's link; the small type is repeated here so this package does not
// depend on the note package.
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
// any) its label.
func ParseLinkInput(s string) LinkInput {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, isSpace)
	if i < 0 {
		return LinkInput{URL: s}
	}
	return LinkInput{URL: s[:i], Label: strings.TrimSpace(s[i:])}
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '　' }

// ValidateLinks reports what buildLinks would refuse, without storing
// anything, so a request can be refused before any of its other changes land.
func ValidateLinks(in []LinkInput) error {
	_, err := buildLinks(in)
	return err
}

// buildLinks validates the inputs and numbers them in order. No links gives
// nil, so a task without links is stored without a "links" key.
func buildLinks(in []LinkInput) ([]Link, error) {
	if len(in) == 0 {
		return nil, nil
	}
	links := make([]Link, 0, len(in))
	for i, l := range in {
		raw := strings.TrimSpace(l.URL)
		u, err := url.Parse(raw)
		if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("関連 URL [%d] は http(s) の URL にしてください（受け取った値: %q）", i+1, raw)
		}
		links = append(links, Link{N: i + 1, URL: raw, Label: strings.TrimSpace(l.Label)})
	}
	return links, nil
}

func sameLinks(a, b []Link) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// encodeLinks is how a link list is kept in an event's from/to value.
func encodeLinks(links []Link) string {
	if len(links) == 0 {
		return ""
	}
	b, _ := json.Marshal(links) // a slice of plain strings and ints always marshals
	return string(b)
}

func decodeLinks(s string) ([]Link, error) {
	if s == "" {
		return nil, nil
	}
	var links []Link
	if err := json.Unmarshal([]byte(s), &links); err != nil {
		return nil, err
	}
	return links, nil
}
