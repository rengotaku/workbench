// Package api is the JSON interface to the memo store. The pages and the CLI
// both go through it, so they always read the same data.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/note-stock/internal/note"
)

// API serves /api.
type API struct {
	store *note.Store
}

// New returns an API over store.
func New(store *note.Store) *API { return &API{store: store} }

// Mount registers the routes on r.
func (a *API) Mount(r gin.IRouter) {
	g := r.Group("/api")
	g.POST("/notes", a.addNote)
	g.GET("/notes", a.listNotes)
	g.GET("/notes/:id", a.getNote)
	g.DELETE("/notes/:id", a.deleteNote)
	g.POST("/notes/:id/annotations", a.annotate)
	g.PUT("/notes/:id/topic", a.setTopic)
	g.GET("/topics", a.listTopics)
	g.POST("/topics", a.addTopic)
}

type errorBody struct {
	Error string `json:"error"`
}

// fail maps a store error to a status and a JSON body whose text says what
// happened and what to do next.
func fail(c *gin.Context, err error) {
	var invalid note.InvalidError
	switch {
	case errors.Is(err, note.ErrNotFound):
		c.JSON(http.StatusNotFound, errorBody{err.Error()})
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, errorBody{err.Error()})
	default:
		slog.Error("store error", "error", err, "path", c.Request.URL.Path)
		c.JSON(http.StatusInternalServerError, errorBody{"保存できませんでした。サーバのログを確認してください: " + err.Error()})
	}
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, errorBody{msg})
}

// noteID reads :id. A value that is not a number is a 400; a number that no
// memo has is left to the store (404).
func noteID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		badRequest(c, "id は 1 以上の数字にしてください")
		return 0, false
	}
	return id, true
}

type addNoteRequest struct {
	Text  string           `json:"text"`
	Topic string           `json:"topic"`
	Links []note.LinkInput `json:"links"`
}

func (a *API) addNote(c *gin.Context) {
	var req addNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "リクエストの JSON を読めません。{\"text\": \"...\", \"links\": [{\"url\": \"...\"}]} の形で送ってください")
		return
	}
	n, err := a.store.Add(req.Text, req.Topic, req.Links...)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, n)
}

// NotesResponse is the body of GET /api/notes.
type NotesResponse struct {
	Notes   []note.Note `json:"notes"`
	Pending int         `json:"pending"`
}

func (a *API) listNotes(c *gin.Context) {
	f := note.Filter{
		Topic:   c.Query("topic"),
		Pending: truthy(c.Query("pending")),
		NoTopic: truthy(c.Query("no_topic")),
	}
	c.JSON(http.StatusOK, NotesResponse{Notes: a.store.List(f), Pending: a.store.PendingCount()})
}

func truthy(v string) bool { return v == "1" || v == "true" }

func (a *API) getNote(c *gin.Context) {
	id, ok := noteID(c)
	if !ok {
		return
	}
	n, err := a.store.Get(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, n)
}

func (a *API) deleteNote(c *gin.Context) {
	id, ok := noteID(c)
	if !ok {
		return
	}
	if err := a.store.Delete(id); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type annotateRequest struct {
	Kind   string `json:"kind"`
	Body   string `json:"body"`
	Source string `json:"source"`
	Author string `json:"author"`
}

func (a *API) annotate(c *gin.Context) {
	id, ok := noteID(c)
	if !ok {
		return
	}
	var req annotateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "リクエストの JSON を読めません。{\"kind\": \"quote\", \"body\": \"...\"} の形で送ってください")
		return
	}
	an, err := a.store.Annotate(id, note.AnnotationInput(req))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, an)
}

// Topic is a pointer so that a missing or null field (a client bug) is not
// mistaken for the explicit empty string that clears the topic.
type topicRequest struct {
	Topic *string `json:"topic"`
}

func (a *API) setTopic(c *gin.Context) {
	id, ok := noteID(c)
	if !ok {
		return
	}
	var req topicRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Topic == nil {
		badRequest(c, "リクエストの JSON を読めません。{\"topic\": \"...\"} の形で送ってください（外すときは空文字。項目を省くと外れません）")
		return
	}
	n, err := a.store.SetTopic(id, *req.Topic)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, n)
}

// TopicsResponse is the body of the /api/topics endpoints.
type TopicsResponse struct {
	Topics []string `json:"topics"`
}

func (a *API) listTopics(c *gin.Context) {
	c.JSON(http.StatusOK, TopicsResponse{Topics: a.store.Topics()})
}

type addTopicRequest struct {
	Name string `json:"name"`
}

func (a *API) addTopic(c *gin.Context) {
	var req addTopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "リクエストの JSON を読めません。{\"name\": \"...\"} の形で送ってください")
		return
	}
	if err := a.store.AddTopic(req.Name); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, TopicsResponse{Topics: a.store.Topics()})
}
