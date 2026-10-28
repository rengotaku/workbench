// Package client talks to a running todo-order server. Every write goes
// through here, so the CLI never touches the data files directly and no change
// can slip past the event log.
package taskclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rengotaku/workbench/internal/task"
)

// DefaultBaseURL is where the server listens unless TODO_ORDER_URL says
// otherwise. Loopback only: the list is not served to the network.
const DefaultBaseURL = "http://127.0.0.1:8085"

// Client is an HTTP client for the task API.
type Client struct {
	http *http.Client
	base string
}

// New returns a client for base, falling back to TODO_ORDER_URL and then to
// DefaultBaseURL.
func New(base string) (*Client, error) {
	if base == "" {
		base = os.Getenv("TODO_ORDER_URL")
	}
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid server URL: %q", base)
	}
	return &Client{
		base: strings.TrimSuffix(base, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// BaseURL reports the server this client talks to, for error messages.
func (c *Client) BaseURL() string { return c.base }

// Task is one task with the labels the server already worked out.
type Task struct {
	DueLabel string `json:"due_label"`
	DueClass string `json:"due_class"`
	task.Task
	StalledFor   int  `json:"stalled_for"`
	InScope      bool `json:"in_scope"`
	Overdue      bool `json:"overdue"`
	UntilReached bool `json:"until_reached"`
}

// LongTask is a long-run task with its next step and child counts.
type LongTask struct {
	Next *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"next"`
	Task
	KidsDone int `json:"kids_done"`
	KidsOpen int `json:"kids_open"`
}

// Board is the whole list, split the way the page shows it.
type Board struct {
	Today   string     `json:"today"`
	Queue   []Task     `json:"queue"`
	Focus   []Task     `json:"focus"`
	Long    []LongTask `json:"long"`
	Waiting []Task     `json:"waiting"`
	Someday []Task     `json:"someday"`
	Done    []Task     `json:"done"`
	Counts  struct {
		Over int `json:"over"`
		Near int `json:"near"`
		Rest int `json:"rest"`
	} `json:"counts"`
	ScopeDays int `json:"scope_days"`
	Revision  int `json:"revision"`
	Undoable  int `json:"undoable"`
}

// List fetches the board for a scope ("week", "2weeks", a number of days, "all", or "").
func (c *Client) List(scope string) (Board, []byte, error) {
	path := "/api/tasks"
	if scope != "" {
		path += "?scope=" + url.QueryEscape(scope)
	}
	var b Board
	raw, err := c.do(http.MethodGet, path, nil, &b)
	return b, raw, err
}

// Add creates a task. links are its related URLs, numbered in the order given.
func (c *Client) Add(name, detail, due string, links ...task.LinkInput) (Task, []byte, error) {
	body := map[string]any{"name": name, "detail": detail, "due": due, "actor": actor()}
	if len(links) > 0 {
		body["links"] = links
	}
	return c.taskCall(http.MethodPost, "/api/tasks", body)
}

// AddChild creates a task tied to a long-run task.
func (c *Client) AddChild(parent, name, detail, due string, links ...task.LinkInput) (Task, []byte, error) {
	body := map[string]any{"name": name, "detail": detail, "due": due, "parent": parent, "actor": actor()}
	if len(links) > 0 {
		body["links"] = links
	}
	return c.taskCall(http.MethodPost, "/api/tasks", body)
}

// Done completes a task.
func (c *Client) Done(id string) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/done?actor="+actor(), nil)
}

// Remove deletes a task and returns what was removed, so the caller can name
// it back to the person who asked.
func (c *Client) Remove(id string) (Task, []byte, error) {
	var wrap struct {
		Removed Task `json:"removed"`
	}
	raw, err := c.do(http.MethodDelete, "/api/tasks/"+url.PathEscape(id)+"?actor="+actor(), nil, &wrap)
	return wrap.Removed, raw, err
}

// Reopen puts a completed task back into the queue.
func (c *Client) Reopen(id string) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/reopen?actor="+actor(), nil)
}

// Focus lifts a task into the band at the top of the page, or takes it out.
func (c *Client) Focus(id string, on bool) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/focus?actor="+actor(),
		map[string]any{"on": on})
}

// Long moves a queue task to the long-run band, or brings it back.
func (c *Client) Long(id string, on bool) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/long", map[string]any{
		"on": on, "actor": actor(),
	})
}

// SetParent ties a task to a parent task; an empty parent unties it.
func (c *Client) SetParent(id, parent string) (Task, []byte, error) {
	return c.taskCall(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), map[string]any{
		"parent": parent, "actor": actor(),
	})
}

// Wait hands a task to someone else.
func (c *Client) Wait(id, to, note string) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/wait", map[string]any{
		"to": to, "note": note, "actor": actor(),
	})
}

// Resume brings a waiting or someday task back into the queue.
func (c *Client) Resume(id, note string) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/resume", map[string]any{
		"note": note, "actor": actor(),
	})
}

// Shelve moves an open or waiting task to someday. An empty until leaves the
// date to the server, which sets it a month out.
func (c *Client) Shelve(id, until string) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/someday", map[string]any{
		"until": until, "actor": actor(),
	})
}

// Order moves a task to a position in the queue. revision guards against a
// list that has moved since it was read; pass 0 to skip the check.
func (c *Client) Order(id string, to, scopeDays, revision int) (Task, []byte, error) {
	return c.taskCall(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/order", map[string]any{
		"to": to, "scope_days": scopeDays, "revision": revision, "actor": actor(),
	})
}

// SetName replaces the task name.
func (c *Client) SetName(id, name string) (Task, []byte, error) {
	return c.taskCall(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), map[string]any{
		"name": name, "actor": actor(),
	})
}

// SetDue sets or clears a due date.
func (c *Client) SetDue(id, due string) (Task, []byte, error) {
	return c.taskCall(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), map[string]any{
		"due": due, "actor": actor(),
	})
}

// SetDetail replaces the detail text.
func (c *Client) SetDetail(id, detail string) (Task, []byte, error) {
	return c.taskCall(http.MethodPatch, "/api/tasks/"+url.PathEscape(id), map[string]any{
		"detail": detail, "actor": actor(),
	})
}

// Annotate appends a supplement to a task. The author is the CLI's actor.
func (c *Client) Annotate(id string, in task.AnnotationInput) (task.Annotation, []byte, error) {
	var wrap struct {
		Annotation task.Annotation `json:"annotation"`
	}
	raw, err := c.do(http.MethodPost, "/api/tasks/"+url.PathEscape(id)+"/annotations", map[string]any{
		"kind": in.Kind, "body": in.Body, "source": in.Source, "actor": actor(),
	}, &wrap)
	return wrap.Annotation, raw, err
}

// Import posts the migration plan built from the old ledgers.
func (c *Client) Import(items []task.ImportItem) (int, []byte, error) {
	var wrap struct {
		Imported int `json:"imported"`
	}
	raw, err := c.do(http.MethodPost, "/api/import", map[string]any{
		"items": items, "actor": actor(),
	}, &wrap)
	return wrap.Imported, raw, err
}

// Undo reverses the last change and says what it put back.
func (c *Client) Undo() (task.Undone, []byte, error) {
	var wrap struct {
		Undone task.Undone `json:"undone"`
	}
	raw, err := c.do(http.MethodPost, "/api/undo?actor="+actor(), nil, &wrap)
	return wrap.Undone, raw, err
}

// Context returns one task with its whole event history.
func (c *Client) Context(id string) (task.Task, []task.Event, []byte, error) {
	var wrap struct {
		Events []task.Event `json:"events"`
		Task   task.Task    `json:"task"`
	}
	raw, err := c.do(http.MethodGet, "/api/tasks/"+url.PathEscape(id)+"/context", nil, &wrap)
	return wrap.Task, wrap.Events, raw, err
}

// Ask returns the changes that still need an explanation.
func (c *Client) Ask() ([]task.Pending, []byte, error) {
	var wrap struct {
		Pending []task.Pending `json:"pending"`
	}
	raw, err := c.do(http.MethodGet, "/api/ask", nil, &wrap)
	return wrap.Pending, raw, err
}

// Why records the reason for one event.
func (c *Client) Why(eventID, reason string) (task.Event, []byte, error) {
	var wrap struct {
		Event task.Event `json:"event"`
	}
	raw, err := c.do(http.MethodPost, "/api/events/"+url.PathEscape(eventID)+"/why",
		map[string]any{"reason": reason}, &wrap)
	return wrap.Event, raw, err
}

func (c *Client) taskCall(method, path string, body map[string]any) (Task, []byte, error) {
	var wrap struct {
		Task Task `json:"task"`
	}
	raw, err := c.do(method, path, body, &wrap)
	return wrap.Task, raw, err
}

// do sends one request. A connection failure is reported as such rather than
// falling back to local files: the server is the only writer, and a silent
// local write would split the source of truth in two.
func (c *Client) do(method, path string, body map[string]any, out any) ([]byte, error) {
	// The server rejects a POST/PUT/PATCH that is not JSON even when it has no
	// body to read, so those methods always send one.
	if body == nil && sendsJSON(method) {
		body = map[string]any{}
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the todo-order server at %s: %w\n"+
			"start it with `make run` (or check the launchd agent)", c.base, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return raw, errors.New(e.Error)
		}
		return raw, fmt.Errorf("server returned %s", res.Status)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("parsing response: %w", err)
		}
	}
	return raw, nil
}

// sendsJSON reports whether the server requires a JSON Content-Type for method.
func sendsJSON(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

// actor marks who made the change. The CLI is used by both a person and an
// assistant, so the caller declares which through TODO_ORDER_ACTOR; the event
// log keeps them apart.
func actor() string {
	if os.Getenv("TODO_ORDER_ACTOR") == string(task.ActorAI) {
		return string(task.ActorAI)
	}
	return string(task.ActorHuman)
}
