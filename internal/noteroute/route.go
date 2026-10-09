// Package noteroute serves POST /api/notes/:id/route: the AI picking up a memo
// and sending it where it belongs. A memo that asks for an action becomes a
// task; one that is only a record stays a memo. It needs both stores, so it
// lives beside noteapi and taskapi instead of inside either.
package noteroute

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/task"
)

// API serves the route endpoint over the two stores.
type API struct {
	notes *note.Store
	tasks *task.Store
	loc   *time.Location
	// mu keeps two requests for the same memo from both looking for its task,
	// finding none, and making two.
	mu sync.Mutex
}

// New returns an API over the two stores. loc is the one the task store uses
// to read due dates.
func New(notes *note.Store, tasks *task.Store, loc *time.Location) *API {
	if loc == nil {
		loc = time.Local
	}
	return &API{notes: notes, tasks: tasks, loc: loc}
}

// Mount registers the route on r.
func (a *API) Mount(r gin.IRouter) {
	r.POST("/api/notes/:id/route", a.route)
}

// TaskInput is the task to make when a memo goes to todo.
type TaskInput struct {
	Name   string `json:"name"`
	Due    string `json:"due,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Request is the body of the route call.
type Request struct {
	Task *TaskInput `json:"task,omitempty"`
	To   string     `json:"to"`
}

// Response is what the call returns: the memo as it is now and, for todo, the
// task made from it.
type Response struct {
	Task *task.Task `json:"task,omitempty"`
	Note note.Note  `json:"note"`
}

type errorBody struct {
	Error string `json:"error"`
}

func bad(c *gin.Context, msg string) { c.JSON(http.StatusBadRequest, errorBody{msg}) }

func (a *API) route(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		bad(c, "id は 1 以上の数字にしてください")
		return
	}
	var req Request
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		bad(c, "リクエストの JSON を読めません。{\"to\": \"record\"} か {\"to\": \"todo\", \"task\": {\"name\": \"...\"}} の形で送ってください")
		return
	}
	if req.To != note.RouteTodo && req.To != note.RouteRecord {
		bad(c, "to は todo / record のどちらかにしてください")
		return
	}
	if req.To == note.RouteTodo {
		if req.Task == nil || strings.TrimSpace(req.Task.Name) == "" {
			bad(c, "todo に振り分けるときは task.name が要ります")
			return
		}
		if req.Task.Due != "" {
			if _, dueErr := task.ParseDate(req.Task.Due, a.loc); dueErr != nil {
				bad(c, "task.due は YYYY-MM-DD の形にしてください")
				return
			}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	n, err := a.notes.Get(id)
	if err != nil {
		a.fail(c, err)
		return
	}
	// Already picked up: the same destination is a no-op that shows the current
	// state, another one is refused.
	if n.Route != nil {
		if n.Route.To != req.To {
			c.JSON(http.StatusConflict, errorBody{"このメモはすでに " + n.Route.To + " に振り分け済みです。振り分け先は変えられません"})
			return
		}
		a.ok(c, n)
		return
	}

	if req.To == note.RouteRecord {
		// A task made for this memo whose memo update failed: the memo belongs
		// to todo, so a record request must not take it away.
		if t, found := a.tasks.FindBySourceNote(id); found {
			c.JSON(http.StatusConflict, errorBody{"このメモからはすでにタスク " + t.ID + " が作られています。記録にはできません。todo として再実行してください"})
			return
		}
		n, err = a.notes.SetRoute(id, note.Route{To: note.RouteRecord})
		if err != nil {
			a.fail(c, err)
			return
		}
		a.ok(c, n)
		return
	}

	// Task first, memo second. If the memo update fails the task stays, and the
	// retry finds it by source_note_id instead of making another.
	t, err := a.tasks.AddFromNote(strings.TrimSpace(req.Task.Name), req.Task.Detail, req.Task.Due, id, task.ActorAI)
	if err != nil {
		slog.Error("route: creating the task failed", "error", err, "note", id)
		c.JSON(http.StatusInternalServerError, errorBody{"タスクを作れませんでした。もう一度実行してください: " + err.Error()})
		return
	}
	n, err = a.notes.SetRoute(id, note.Route{To: note.RouteTodo, TaskID: t.ID})
	if err != nil {
		a.fail(c, err)
		return
	}
	a.ok(c, n)
}

// ok answers with the memo and, for todo, the task it points to.
func (a *API) ok(c *gin.Context, n note.Note) {
	res := Response{Note: n}
	if n.Route != nil && n.Route.To == note.RouteTodo {
		if t, err := a.tasks.Get(n.Route.TaskID); err == nil {
			res.Task = &t
		}
	}
	c.JSON(http.StatusOK, res)
}

func (a *API) fail(c *gin.Context, err error) {
	var invalid note.InvalidError
	switch {
	case errors.Is(err, note.ErrNotFound):
		c.JSON(http.StatusNotFound, errorBody{err.Error()})
	case errors.Is(err, note.ErrRouteConflict):
		c.JSON(http.StatusConflict, errorBody{err.Error()})
	case errors.As(err, &invalid):
		bad(c, err.Error())
	default:
		slog.Error("route: store error", "error", err, "path", c.Request.URL.Path)
		c.JSON(http.StatusInternalServerError, errorBody{"保存できませんでした。もう一度実行してください（タスクは作成済みの場合、再実行で同じタスクを使います）: " + err.Error()})
	}
}
