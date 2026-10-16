// Package api exposes the task store over HTTP. It is the only writer, so
// the CLI and the page cannot disagree about what happened or skip an event.
package taskapi

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/task"
)

// DefaultScopeDays is the window the page opens with: one week.
const DefaultScopeDays = 7

// API serves the task endpoints.
type API struct {
	store *task.Store
	loc   *time.Location
}

// New returns an API backed by store.
func New(store *task.Store, loc *time.Location) *API {
	if loc == nil {
		loc = time.Local
	}
	return &API{store: store, loc: loc}
}

// Mount registers the endpoints under /api.
func (a *API) Mount(r gin.IRouter) {
	g := r.Group("/api")

	g.GET("/tasks", a.listTasks)
	g.GET("/revision", a.revision)
	g.POST("/undo", a.undo)
	g.POST("/tasks", a.addTask)
	g.PATCH("/tasks/:id", a.patchTask)
	g.DELETE("/tasks/:id", a.removeTask)
	g.POST("/tasks/:id/done", a.doneTask)
	g.POST("/tasks/:id/focus", a.focusTask)
	g.POST("/tasks/:id/reopen", a.reopenTask)
	g.POST("/tasks/:id/wait", a.waitTask)
	g.POST("/tasks/:id/resume", a.resumeTask)
	g.POST("/tasks/:id/someday", a.somedayTask)
	g.POST("/tasks/:id/order", a.orderTask)
	g.GET("/tasks/:id/context", a.taskContext)
	g.POST("/tasks/:id/annotations", a.annotateTask)

	g.POST("/import", a.importTasks)

	g.GET("/ask", a.ask)
	g.POST("/events/:id/why", a.why)
}

// View is a task plus the presentation facts the page and CLI both need, so
// neither has to re-derive the due-date arithmetic.
type View struct {
	DueLabel string `json:"due_label"`
	DueClass string `json:"due_class"`
	task.Task
	StalledFor   int  `json:"stalled_for,omitempty"`
	InScope      bool `json:"in_scope"`
	Overdue      bool `json:"overdue"`
	UntilReached bool `json:"until_reached,omitempty"`
}

type listResponse struct {
	Today   string      `json:"today"`
	Queue   []View      `json:"queue"`
	Focus   []View      `json:"focus"`
	Waiting []View      `json:"waiting"`
	Someday []View      `json:"someday"`
	Done    []View      `json:"done"`
	Dropped []task.Drop `json:"dropped"`
	// Counts splits the queue the same way as the row stripe (ADR todo-0011):
	// over = start date passed, near = start date is today, rest = everything
	// else. The keys keep their names because the page and the CLI read them.
	Counts struct {
		Over int `json:"over"`
		Near int `json:"near"`
		Rest int `json:"rest"`
	} `json:"counts"`
	ScopeDays int `json:"scope_days"`
	Revision  int `json:"revision"`
	Undoable  int `json:"undoable"`
}

func (a *API) listTasks(c *gin.Context) {
	scope, err := scopeDays(c.Query("scope"))
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, a.build(scope))
}

func (a *API) build(scope int) listResponse {
	today := a.today()
	var res listResponse
	res.Today = today.Format(task.DateLayout)
	res.ScopeDays = scope

	// Completions older than the most recent Monday drop off the page. The
	// week boundary is a query, not a migration — nothing is moved on disk.
	weekStart := mondayOf(today)

	tasks, rev := a.store.List()
	res.Revision = rev
	// Dropped tasks are read from the log, not the list: they are gone from
	// tasks.json by the time anyone wants to see what was given up on.
	if dropped, err := a.store.Dropped(weekStart); err == nil {
		res.Dropped = dropped
	}
	if n, err := a.store.UndoableCount(); err == nil {
		res.Undoable = n
	}

	for _, t := range tasks {
		v := a.view(t, today, scope)
		switch t.State {
		case task.StateWaiting:
			// Focused stays set, but the band is what to do next and the ball
			// is with someone else; resuming puts it back in the band.
			res.Waiting = append(res.Waiting, v)
		case task.StateSomeday:
			res.Someday = append(res.Someday, v)
		case task.StateDone:
			if t.CompletedAt != nil && t.CompletedAt.In(a.loc).Before(weekStart) {
				continue
			}
			res.Done = append(res.Done, v)
		default:
			res.Queue = append(res.Queue, v)
			if t.Focused {
				res.Focus = append(res.Focus, v)
			}
			switch v.DueClass {
			case "over":
				res.Counts.Over++
			case "today":
				res.Counts.Near++
			default:
				res.Counts.Rest++
			}
		}
	}

	// Someday reads as a shelf, not a queue: what to look at soonest comes
	// first, and what has no date at all trails behind everything that does.
	sort.SliceStable(res.Someday, func(i, j int) bool {
		u, v := res.Someday[i].Until, res.Someday[j].Until
		if u == "" {
			return false
		}
		if v == "" {
			return true
		}
		return u < v
	})
	return res
}

// undo reverses the last change, whoever made it: one entry point for the page
// and the CLI, so undoing does not depend on which one did the thing.
func (a *API) undo(c *gin.Context) {
	done, err := a.store.Undo(actorOf(c.Query("actor")))
	if err != nil {
		if errors.Is(err, task.ErrNothingToUndo) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"undone": done})
}

// revision is what the page polls to notice a change made elsewhere — by the
// CLI, or by another window — without re-fetching the whole list. It also
// carries today: the due colours change at midnight with no write, so the page
// reloads when the date it was drawn for is no longer today.
func (a *API) revision(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"revision": a.store.Revision(),
		"today":    a.today().Format(task.DateLayout),
	})
}

func (a *API) view(t task.Task, today time.Time, scope int) View {
	v := View{Task: t}

	if days, ok := t.StalledFor(today); ok {
		v.StalledFor = days
	}
	if t.State == task.StateSomeday {
		v.UntilReached = untilReached(t.Until, today, a.loc)
	}

	if t.Due == "" {
		// A task with no due date is not "past the window" — there is no date
		// to be past. Collapsing it would hide the top of the queue, so it
		// stays visible and only counts as neither overdue nor in-window.
		v.DueLabel, v.DueClass = "期限なし", "none"
		v.InScope = true
		return v
	}
	d, err := task.ParseDate(t.Due, a.loc)
	if err != nil {
		v.DueLabel, v.DueClass = t.Due, "none"
		return v
	}
	diff := int(d.Sub(today).Hours() / 24)
	switch {
	case diff < 0:
		v.DueLabel = strconv.Itoa(-diff) + "日超過"
		v.DueClass = "over"
		v.Overdue, v.InScope = true, true
	case diff == 0:
		// The due date is when to start. Today is the day to start, which is
		// not the same as having let it pass, so it gets its own class; it
		// still counts as overdue for the header numbers.
		v.DueLabel, v.DueClass = "今日", "today"
		v.Overdue, v.InScope = true, true
	case diff <= scope:
		v.DueLabel = "あと" + strconv.Itoa(diff) + "日"
		v.DueClass = "near"
		v.InScope = true
	default:
		v.DueLabel = d.Format("1/2")
		v.DueClass = "plain"
	}
	return v
}

type addRequest struct {
	Name     string `json:"name"`
	Detail   string `json:"detail"`
	URL      string `json:"url"`
	LinkText string `json:"link_text"`
	Due      string `json:"due"`
	Actor    string `json:"actor"`
	// Links are the related URLs, numbered 1..k in the order given.
	Links []task.LinkInput `json:"links"`
}

func (a *API) addTask(c *gin.Context) {
	var req addRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	actor := actorOf(req.Actor)
	var (
		t   task.Task
		err error
	)
	if len(req.Links) > 0 {
		t, err = a.store.AddWithLinks(req.Name, req.Detail, req.Due, req.Links, actor)
	} else {
		t, err = a.store.AddLinked(req.Name, req.Detail, req.Due, req.URL, req.LinkText, actor)
	}
	a.respond(c, t, err)
}

type patchRequest struct {
	Name   *string `json:"name"`
	Due    *string `json:"due"`
	Detail *string `json:"detail"`
	URL    *string `json:"url"`
	// Links is the task's full new list; absent leaves it alone, [] clears it.
	Links    *[]task.LinkInput `json:"links"`
	LinkText string            `json:"link_text"`
	Actor    string            `json:"actor"`
}

func (a *API) patchTask(c *gin.Context) {
	var req patchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	id := c.Param("id")
	actor := actorOf(req.Actor)
	// Invalid links refuse the whole request: checked before the name or the
	// due date change, so a 400 never leaves half of it applied.
	if req.Links != nil {
		if err := task.ValidateLinks(*req.Links); err != nil {
			a.fail(c, err)
			return
		}
	}

	var (
		t   task.Task
		err error
	)
	if req.Name != nil {
		if t, err = a.store.SetName(id, *req.Name, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Due != nil {
		if t, err = a.store.SetDue(id, *req.Due, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Detail != nil {
		if t, err = a.store.SetDetail(id, *req.Detail, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.URL != nil {
		if t, err = a.store.SetLink(id, *req.URL, req.LinkText, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Links != nil {
		if t, err = a.store.SetLinks(id, *req.Links, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Name == nil && req.Due == nil && req.Detail == nil && req.URL == nil && req.Links == nil {
		badRequest(c, errors.New("nothing to change: pass name, due, detail, url or links"))
		return
	}
	a.respond(c, t, nil)
}

// removeTask deletes a task. It is the only call that loses data, so it is a
// DELETE and nothing else routes to it by accident.
func (a *API) removeTask(c *gin.Context) {
	t, err := a.store.Remove(c.Param("id"), actorOf(c.Query("actor")))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": t})
}

func (a *API) doneTask(c *gin.Context) {
	t, err := a.store.Done(c.Param("id"), actorOf(c.Query("actor")))
	a.respond(c, t, err)
}

// reopenTask undoes a completion. Nothing is erased: the reopen is its own
// event, so the trail still shows it was finished and then put back.
func (a *API) reopenTask(c *gin.Context) {
	t, err := a.store.Reopen(c.Param("id"), actorOf(c.Query("actor")))
	a.respond(c, t, err)
}

type focusRequest struct {
	On *bool `json:"on"`
	// To is the queue position the task was dropped at. Without it the task
	// goes to the edge of the focused block.
	To *int `json:"to"`
	// Revision is the one the drop position was read at, as for /order.
	Revision *int `json:"revision"`
}

// focusTask lifts a task into the band at the top, or takes it out. Focused
// tasks are the top of the queue (ADR todo-0012), so this moves the task too.
func (a *API) focusTask(c *gin.Context) {
	var req focusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	if req.On == nil {
		badRequest(c, errors.New("pass on: true or false"))
		return
	}
	to := -1
	if req.To != nil {
		if *req.To < 0 {
			badRequest(c, errors.New("to must be 0 or more"))
			return
		}
		to = *req.To
	}
	expect := 0
	if req.Revision != nil {
		expect = *req.Revision
	}
	t, err := a.store.FocusAt(c.Param("id"), *req.On, to, expect, actorOf(c.Query("actor")))
	a.respond(c, t, err)
}

type waitRequest struct {
	To    string `json:"to"`
	Note  string `json:"note"`
	Actor string `json:"actor"`
}

func (a *API) waitTask(c *gin.Context) {
	var req waitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	t, err := a.store.Wait(c.Param("id"), req.To, req.Note, actorOf(req.Actor))
	a.respond(c, t, err)
}

type resumeRequest struct {
	Note  string `json:"note"`
	Actor string `json:"actor"`
}

func (a *API) resumeTask(c *gin.Context) {
	var req resumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	t, err := a.store.Resume(c.Param("id"), req.Note, actorOf(req.Actor))
	a.respond(c, t, err)
}

type somedayRequest struct {
	Until string `json:"until"`
	Actor string `json:"actor"`
}

// somedayTask shelves an open or waiting task. Arrival day and count are
// worked out here, the same as the due-date arithmetic, so the page and the
// CLI never have to recompute either.
func (a *API) somedayTask(c *gin.Context) {
	var req somedayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	t, err := a.store.Shelve(c.Param("id"), req.Until, actorOf(req.Actor))
	a.respond(c, t, err)
}

type orderRequest struct {
	ScopeDays *int   `json:"scope_days"`
	Revision  *int   `json:"revision"`
	Actor     string `json:"actor"`
	To        int    `json:"to"`
}

func (a *API) orderTask(c *gin.Context) {
	var req orderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	scope := DefaultScopeDays
	if req.ScopeDays != nil {
		scope = *req.ScopeDays
	}
	expect := 0
	if req.Revision != nil {
		expect = *req.Revision
	}
	t, err := a.store.Reorder(c.Param("id"), req.To, scope, expect, actorOf(req.Actor))
	a.respond(c, t, err)
}

type importRequest struct {
	Actor string            `json:"actor"`
	Items []task.ImportItem `json:"items"`
}

// importTasks brings the old ledgers over. The server stays the only writer,
// so the migration goes through here rather than editing the files directly.
func (a *API) importTasks(c *gin.Context) {
	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	tasks, err := a.store.Import(req.Items, actorOf(req.Actor))
	if err != nil {
		if errors.Is(err, task.ErrNotEmpty) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "imported": len(tasks)})
}

type annotateRequest struct {
	Kind   string `json:"kind"`
	Body   string `json:"body"`
	Source string `json:"source"`
	Actor  string `json:"actor"`
}

// annotateTask appends a supplement. It never changes the task itself and is
// not something undo reverses.
func (a *API) annotateTask(c *gin.Context) {
	var req annotateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	actor := req.Actor
	if actor == "" {
		actor = c.Query("actor")
	}
	an, err := a.store.Annotate(c.Param("id"), task.AnnotationInput{Kind: req.Kind, Body: req.Body, Source: req.Source}, actorOf(actor))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"annotation": an})
}

func (a *API) taskContext(c *gin.Context) {
	id := c.Param("id")
	t, err := a.store.Get(id)
	if err != nil {
		a.fail(c, err)
		return
	}
	events, err := a.store.Events(id)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": t, "events": events})
}

func (a *API) ask(c *gin.Context) {
	pending, err := a.store.Ask()
	if err != nil {
		a.fail(c, err)
		return
	}
	if pending == nil {
		pending = []task.Pending{}
	}
	c.JSON(http.StatusOK, gin.H{"pending": pending})
}

type whyRequest struct {
	Reason string `json:"reason"`
}

func (a *API) why(c *gin.Context) {
	var req whyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	e, err := a.store.Why(c.Param("id"), req.Reason)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"event": e})
}

// ---- helpers ----

func (a *API) today() time.Time {
	n := time.Now().In(a.loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, a.loc)
}

func (a *API) respond(c *gin.Context, t task.Task, err error) {
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": t})
}

// fail maps store errors onto status codes. A missing or already-completed ID
// is reported as an error rather than a quiet success, so a mistyped ID cannot
// look like it worked.
func (a *API) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, task.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, task.ErrAlreadyDone):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, task.ErrStale):
		// 409 rather than 400: nothing is wrong with the request, the list
		// simply moved. The caller re-reads and decides again.
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "stale": true})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func badRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func actorOf(s string) task.Actor {
	if s == string(task.ActorAI) {
		return task.ActorAI
	}
	return task.ActorHuman
}

// scopeDays parses the scope query. "all" keeps everything in view.
func scopeDays(raw string) (int, error) {
	if raw == "" {
		return DefaultScopeDays, nil
	}
	if raw == "all" {
		return 36500, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, errors.New(`scope must be a number of days or "all"`)
	}
	return n, nil
}

// untilReached reports whether a someday task's until date has come: today or
// earlier. Coming true is not "overdue" — it just means it is time to look at
// it again — so it is a field of its own rather than borrowing Overdue.
func untilReached(until string, today time.Time, loc *time.Location) bool {
	if until == "" {
		return false
	}
	d, err := task.ParseDate(until, loc)
	if err != nil {
		return false
	}
	return !d.After(today)
}

// mondayOf returns the most recent Monday at midnight, which is where the
// "done this week" list starts.
func mondayOf(today time.Time) time.Time {
	weekday := int(today.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday closes the week that began the previous Monday
	}
	return today.AddDate(0, 0, -(weekday - 1))
}
