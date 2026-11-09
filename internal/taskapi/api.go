// Package api exposes the task store over HTTP. It is the only writer, so
// the CLI and the page cannot disagree about what happened or skip an event.
package taskapi

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/task"
)

// DefaultScope is the window the page opens with: this calendar week, to
// Sunday. A week counted from today would let next week's tasks in one day at
// a time; a calendar week matches "this week".
const DefaultScope = "week"

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
	g.POST("/tasks/:id/long", a.longTask)
	g.POST("/tasks/:id/review", a.reviewTask)
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
	// IdleFrom says which date IdleDays counts from: "due", "annotation", "review" or
	// "created" (ADR todo-0021).
	IdleFrom string `json:"idle_from,omitempty"`
	// ParentName and ParentPath describe the parent, so the page needs no
	// lookup: the path runs from the top of the tree down to the parent.
	ParentName string    `json:"parent_name,omitempty"`
	ParentPath []PathRef `json:"parent_path,omitempty"`
	task.Task
	// KidsDone and KidsOpen count the children, across every week (ADR
	// todo-0020). Both are left out while there are no children.
	KidsDone   int `json:"kids_done,omitempty"`
	KidsOpen   int `json:"kids_open,omitempty"`
	StalledFor int `json:"stalled_for,omitempty"`
	// IdleDays is how many days a task has been left without a person
	// touching it, and IdleStage the step the page colours it by. Both are
	// left out when there is nothing to show.
	IdleDays     int  `json:"idle_days,omitempty"`
	IdleStage    int  `json:"idle_stage,omitempty"`
	InScope      bool `json:"in_scope"`
	Overdue      bool `json:"overdue"`
	UntilReached bool `json:"until_reached,omitempty"`
}

// PathRef is one task on the way from the top of a tree down to a parent.
type PathRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Long marks a long-run task, which the page writes as 長期.
	Long bool `json:"long,omitempty"`
}

// LongView is a long-run task with what the band shows under it: the next
// step (the open child highest in the queue) and how many children are done
// and not done. Children are counted across every week, not only this one.
type LongView struct {
	Next *ChildRef `json:"next,omitempty"`
	// ReviewReasons says why the task needs a review: no_next, review_due or
	// stalled (ADR todo-0022). The screen and the CLI do not work these out.
	ReviewReasons []string `json:"review_reasons,omitempty"`
	View
	KidsDone    int  `json:"kids_done"`
	KidsOpen    int  `json:"kids_open"`
	NeedsReview bool `json:"needs_review"`
}

// ChildRef names a child task without carrying all of it.
type ChildRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listResponse struct {
	// NextDue is the nearest due date after today among the queue (focus
	// included), so the page names it without scanning the rows. Nil when no
	// queue task has a later due date (ADR todo-0023).
	NextDue *NextDue    `json:"next_due,omitempty"`
	Today   string      `json:"today"`
	Queue   []View      `json:"queue"`
	Focus   []View      `json:"focus"`
	Long    []LongView  `json:"long"`
	Waiting []View      `json:"waiting"`
	Someday []View      `json:"someday"`
	Done    []View      `json:"done"`
	Dropped []task.Drop `json:"dropped"`
	// Topics are the topics in use, each once, in the order they first appear
	// down the queue (focus first), then long, waiting and someday. They feed
	// the page's input suggestions (ADR todo-0024).
	Topics []string `json:"topics"`
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
	scope, err := scopeDays(c.Query("scope"), a.today())
	if err != nil {
		badRequest(c, err)
		return
	}
	c.JSON(http.StatusOK, a.build(scope))
}

// NextDue names the queue task whose due date is the nearest one after today.
type NextDue struct {
	Due  string `json:"due"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// nextDue picks the earliest due date strictly after today; among the same
// day the first in queue order wins.
func nextDue(queue []View, today string) *NextDue {
	var best *NextDue
	for _, v := range queue {
		if v.Due == "" || v.Due <= today {
			continue
		}
		if best == nil || v.Due < best.Due {
			best = &NextDue{Due: v.Due, ID: v.ID, Name: v.Name}
		}
	}
	return best
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
	kids := countKids(tasks)
	byID := make(map[string]task.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
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
		v.describeTree(byID, kids)
		a.describeIdle(&v, t, today)
		switch t.State {
		case task.StateWaiting:
			// Focused stays set, but the band is what to do next and the ball
			// is with someone else; resuming puts it back in the band.
			res.Waiting = append(res.Waiting, v)
		case task.StateSomeday:
			res.Someday = append(res.Someday, v)
		case task.StateLong:
			res.Long = append(res.Long, longView(v, kids, res.Today))
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

	res.NextDue = nextDue(res.Queue, res.Today)
	res.Topics = topicsInUse(res)

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

// topicsInUse lists each topic once, in the order it first appears down the
// queue, then the long, waiting and someday rows. Never nil, so the JSON is [].
func topicsInUse(res listResponse) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(t string) {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, v := range res.Queue {
		add(v.Topic)
	}
	for _, v := range res.Long {
		add(v.Topic)
	}
	for _, v := range res.Waiting {
		add(v.Topic)
	}
	for _, v := range res.Someday {
		add(v.Topic)
	}
	return out
}

// kidCount is what the children of one task add up to.
type kidCount struct {
	next       *ChildRef
	done, open int
}

// countKids tallies every task's children. tasks is the whole list in Order,
// so the first open child met is the one highest in the queue.
func countKids(tasks []task.Task) map[string]*kidCount {
	out := map[string]*kidCount{}
	for _, t := range tasks {
		if t.Parent == "" {
			continue
		}
		k := out[t.Parent]
		if k == nil {
			k = &kidCount{}
			out[t.Parent] = k
		}
		if t.State == task.StateDone {
			k.done++
			continue
		}
		k.open++
		if k.next == nil && t.State == task.StateOpen {
			k.next = &ChildRef{ID: t.ID, Name: t.Name}
		}
	}
	return out
}

// describeTree fills in the parent's name and path and the child counts.
func (v *View) describeTree(byID map[string]task.Task, kids map[string]*kidCount) {
	if k := kids[v.ID]; k != nil {
		v.KidsDone, v.KidsOpen = k.done, k.open
	}
	var path []PathRef
	for cur, hops := v.Parent, 0; cur != "" && hops < task.MaxDepth*4; hops++ {
		p, ok := byID[cur]
		if !ok {
			break
		}
		path = append([]PathRef{{ID: p.ID, Name: p.Name, Long: p.State == task.StateLong}}, path...)
		cur = p.Parent
	}
	if len(path) > 0 {
		v.ParentPath = path
		v.ParentName = path[len(path)-1].Name
	}
}

// longView adds the next step to a long-run task: the open child highest in
// the queue. The counts come with every row, long-run or not.
func longView(v View, kids map[string]*kidCount, today string) LongView {
	lv := LongView{View: v, KidsDone: v.KidsDone, KidsOpen: v.KidsOpen}
	if k := kids[v.ID]; k != nil {
		lv.Next = k.next
	}
	if lv.KidsOpen == 0 {
		lv.ReviewReasons = append(lv.ReviewReasons, "no_next")
	}
	// A task with no review date counts as due, so an old one surfaces once.
	if v.ReviewOn == "" || v.ReviewOn <= today {
		lv.ReviewReasons = append(lv.ReviewReasons, "review_due")
	}
	if v.IdleDays >= StalledDays {
		lv.ReviewReasons = append(lv.ReviewReasons, "stalled")
	}
	lv.NeedsReview = len(lv.ReviewReasons) > 0
	return lv
}

// StalledDays is the idle length at which a long-run task needs a review.
const StalledDays = 8

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
		v.DueLabel = Span(-diff) + "超過"
		v.DueClass = "over"
		v.Overdue, v.InScope = true, true
	case diff == 0:
		// The due date is when to start. Today is the day to start, which is
		// not the same as having let it pass, so it gets its own class; it
		// still counts as overdue for the header numbers.
		v.DueLabel, v.DueClass = "今日", "today"
		v.Overdue, v.InScope = true, true
	case diff <= scope:
		// A start date still ahead is read as a date, not a countdown: the
		// row is not due yet (ADR todo-0023). 1-3 days out is "soon" and gets
		// the amber stripe; further in scope stays plain.
		v.DueLabel = fromLabel(d)
		v.DueClass = "near"
		if diff <= SoonDays {
			v.DueClass = "soon"
		}
		v.InScope = true
	default:
		// The screen sorts these by due date and shows the days left on hover.
		v.DueLabel = fromLabel(d)
		v.DueClass = "plain"
		if diff <= SoonDays {
			v.DueClass = "soon"
		}
	}
	return v
}

// SoonDays is how many days ahead a due date counts as "soon" (due_class
// "soon"): the row gets the amber stripe from then on (ADR todo-0023).
const SoonDays = 3

// fromLabel is the chip text for a start date that has not come: "11/5 から".
func fromLabel(d time.Time) string {
	return strconv.Itoa(int(d.Month())) + "/" + strconv.Itoa(d.Day()) + " から"
}

// Span names a count of days in the unit that suits its size, the way a chat
// says "3 weeks ago" instead of "23 days ago": days up to 6, then weeks up to
// 27, months (30 days, at least 1) up to 364, then years. It rounds down.
func Span(days int) string {
	switch {
	case days >= 365:
		return strconv.Itoa(days/365) + "年"
	case days >= 28:
		return strconv.Itoa(max(1, days/30)) + "か月"
	case days >= 7:
		return strconv.Itoa(days/7) + "週間"
	default:
		return strconv.Itoa(days) + "日"
	}
}

type addRequest struct {
	Name     string `json:"name"`
	Detail   string `json:"detail"`
	URL      string `json:"url"`
	LinkText string `json:"link_text"`
	Due      string `json:"due"`
	// Parent is the task the new one is cut out of.
	Parent string `json:"parent"`
	Actor  string `json:"actor"`
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
	switch {
	case req.Parent != "":
		// One change: a bad parent refuses the add, and one undo removes it.
		t, err = a.store.AddChild(req.Name, req.Detail, req.Due, req.Parent, req.Links, req.URL, req.LinkText, actor)
	case len(req.Links) > 0:
		t, err = a.store.AddWithLinks(req.Name, req.Detail, req.Due, req.Links, actor)
	default:
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
	Links *[]task.LinkInput `json:"links"`
	// Parent ties the task to another task; "" unties it.
	Parent *string `json:"parent"`
	// Topic sets the topic; "" clears it (ADR todo-0024).
	Topic    *string `json:"topic"`
	LinkText string  `json:"link_text"`
	Actor    string  `json:"actor"`
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
	if req.Topic != nil {
		if _, err := task.NormalizeTopic(*req.Topic); err != nil {
			badRequest(c, err)
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
	if req.Parent != nil {
		if t, err = a.store.SetParent(id, *req.Parent, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Topic != nil {
		if t, err = a.store.SetTopic(id, *req.Topic, actor); err != nil {
			a.respond(c, t, err)
			return
		}
	}
	if req.Name == nil && req.Due == nil && req.Detail == nil && req.URL == nil && req.Links == nil && req.Parent == nil && req.Topic == nil {
		badRequest(c, errors.New("nothing to change: pass name, due, detail, url, links, parent or topic"))
		return
	}
	a.respond(c, t, nil)
}

type removeRequest struct {
	Reason string `json:"reason"`
}

// removeTask deletes a task. It is the only call that loses data, so it is a
// DELETE and nothing else routes to it by accident. The body is optional: the
// CLI sends none, the screen sends the reason it asked for.
func (a *API) removeTask(c *gin.Context) {
	var req removeRequest
	if c.Request.ContentLength != 0 {
		// A chunked body of unknown length may still turn out empty.
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			badRequest(c, err)
			return
		}
	}
	t, err := a.store.Remove(c.Param("id"), actorOf(c.Query("actor")), req.Reason)
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

type longRequest struct {
	On    *bool  `json:"on"`
	Actor string `json:"actor"`
}

// longTask moves a queue task to the long-run band, or brings it back to the
// top of the queue (ADR todo-0019).
func (a *API) longTask(c *gin.Context) {
	var req longRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	if req.On == nil {
		badRequest(c, errors.New("pass on: true or false"))
		return
	}
	t, err := a.store.SetLong(c.Param("id"), *req.On, actorOf(req.Actor))
	a.respond(c, t, err)
}

type reviewRequest struct {
	ReviewOn string `json:"review_on"`
	Actor    string `json:"actor"`
}

// reviewTask sets a long-run task's next review date (a week out when none is
// given) and notes that it has just been looked at (ADR todo-0022).
func (a *API) reviewTask(c *gin.Context) {
	var req reviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	t, err := a.store.Review(c.Param("id"), req.ReviewOn, actorOf(req.Actor))
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
	scope, _ := scopeDays(DefaultScope, a.today())
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
	case errors.Is(err, task.ErrOrderRule):
		// Like a stale list, the request is sound and the list is unchanged;
		// the page shows the message and draws the list again.
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "rule": true})
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

// scopeDays parses the scope query into days from today. "week" and "2weeks"
// end on Sunday of this week and of next week; "all" keeps everything in view.
func scopeDays(raw string, today time.Time) (int, error) {
	if raw == "" {
		raw = DefaultScope
	}
	switch raw {
	case "week":
		return daysToSunday(today, 1), nil
	case "2weeks":
		return daysToSunday(today, 2), nil
	case "all":
		return 36500, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, errors.New(`scope must be a number of days, "week", "2weeks" or "all"`)
	}
	return n, nil
}

// daysToSunday counts the days from today to Sunday of the weeks-th week,
// this week being the first: on a Monday, 6 for one week and 13 for two.
func daysToSunday(today time.Time, weeks int) int {
	sunday := mondayOf(today).AddDate(0, 0, 7*weeks-1)
	return int(sunday.Sub(today).Hours() / 24)
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
