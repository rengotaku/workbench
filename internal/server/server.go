// Package server assembles the todo and note screens and APIs into one gin
// engine, behind one httpguard and one /healthz.
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/httpguard"
	"github.com/rengotaku/workbench/internal/notepages"
	"github.com/rengotaku/workbench/internal/taskweb"
)

// Mounter is anything that registers its own routes, such as the call that
// needs both the task and the note store.
type Mounter interface{ Mount(r gin.IRouter) }

// New returns the router. port is the one the server listens on; requests for
// any other Host or Origin are refused. extra are mounted after the screens.
func New(port string, todo *taskweb.Handler, notes *notepages.Pages, extra ...Mounter) http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), httpguard.New(port))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	todo.Mount(r)
	notes.Mount(r)
	for _, m := range extra {
		m.Mount(r)
	}
	return r
}
