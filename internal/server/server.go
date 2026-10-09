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

// New returns the router. port is the one the server listens on; requests for
// any other Host or Origin are refused.
func New(port string, todo *taskweb.Handler, notes *notepages.Pages) http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), httpguard.New(port))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	todo.Mount(r)
	notes.Mount(r)
	return r
}
