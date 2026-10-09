// Package client is the CLI's way to the memo store: the HTTP API of the
// running server. There is no fallback that writes the files directly, so the
// server stays the only writer.
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rengotaku/note-stock/internal/api"
	"github.com/rengotaku/note-stock/internal/note"
)

// DefaultBaseURL is where the server listens unless NOTE_SERVER says otherwise.
const DefaultBaseURL = "http://127.0.0.1:8086"

// Client talks to one server.
type Client struct {
	http *http.Client
	base string
}

// New returns a client for base; "" means $NOTE_SERVER, then DefaultBaseURL.
func New(base string) (*Client, error) {
	if base == "" {
		base = os.Getenv("NOTE_SERVER")
	}
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("サーバの URL が正しくありません: %q（http://127.0.0.1:8086 の形で指定してください）", base)
	}
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 10 * time.Second}}, nil
}

// BaseURL is the server address with no trailing slash.
func (c *Client) BaseURL() string { return c.base }

// do sends one request and returns the raw response body. A non-2xx answer
// becomes an error carrying the server's own message.
func (c *Client) do(method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("送る内容を作れません: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("リクエストを作れません: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("サーバに接続できません（%s）。note-stock のサーバが起動しているか確認してください: %w", c.base, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("サーバの応答を読めません: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return nil, fmt.Errorf("サーバがエラーを返しました（HTTP %d）", res.StatusCode)
	}
	return raw, nil
}

func decode[T any](raw []byte) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("サーバの応答を解釈できません: %w", err)
	}
	return v, nil
}

// Add stores a memo with its related links, numbered in the order given.
func (c *Client) Add(text, topic string, links ...note.LinkInput) (note.Note, []byte, error) {
	body := map[string]any{"text": text, "topic": topic}
	if len(links) > 0 {
		body["links"] = links
	}
	raw, err := c.do(http.MethodPost, "/api/notes", body)
	if err != nil {
		return note.Note{}, nil, err
	}
	n, err := decode[note.Note](raw)
	return n, raw, err
}

// List returns memos, newest first.
func (c *Client) List(f note.Filter) (api.NotesResponse, []byte, error) {
	q := url.Values{}
	if f.Topic != "" {
		q.Set("topic", f.Topic)
	}
	if f.Pending {
		q.Set("pending", "1")
	}
	if f.NoTopic {
		q.Set("no_topic", "1")
	}
	path := "/api/notes"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	raw, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return api.NotesResponse{}, nil, err
	}
	r, err := decode[api.NotesResponse](raw)
	return r, raw, err
}

// Get returns one memo with its annotations.
func (c *Client) Get(id int) (note.Note, []byte, error) {
	raw, err := c.do(http.MethodGet, "/api/notes/"+strconv.Itoa(id), nil)
	if err != nil {
		return note.Note{}, nil, err
	}
	n, err := decode[note.Note](raw)
	return n, raw, err
}

// Annotate appends a supplement.
func (c *Client) Annotate(id int, in note.AnnotationInput) (note.Annotation, error) {
	raw, err := c.do(http.MethodPost, "/api/notes/"+strconv.Itoa(id)+"/annotations", map[string]string{
		"kind": in.Kind, "body": in.Body, "source": in.Source, "author": in.Author,
	})
	if err != nil {
		return note.Annotation{}, err
	}
	return decode[note.Annotation](raw)
}

// SetTopic sets or (with "") clears a memo's topic.
func (c *Client) SetTopic(id int, topic string) (note.Note, error) {
	raw, err := c.do(http.MethodPut, "/api/notes/"+strconv.Itoa(id)+"/topic", map[string]string{"topic": topic})
	if err != nil {
		return note.Note{}, err
	}
	return decode[note.Note](raw)
}

// Topics lists the known topics.
func (c *Client) Topics() ([]string, error) {
	raw, err := c.do(http.MethodGet, "/api/topics", nil)
	if err != nil {
		return nil, err
	}
	r, err := decode[api.TopicsResponse](raw)
	return r.Topics, err
}

// Delete removes a memo.
func (c *Client) Delete(id int) error {
	_, err := c.do(http.MethodDelete, "/api/notes/"+strconv.Itoa(id), nil)
	return err
}
