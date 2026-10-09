// Package server assembles the todo and note screens and APIs into one gin
// engine, behind one httpguard and one /healthz.
package server

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/httpguard"
	"github.com/rengotaku/workbench/internal/notepages"
	"github.com/rengotaku/workbench/internal/taskweb"
	sharedweb "github.com/rengotaku/workbench/web/shared"
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
	// The layout, tokens and common script both halves use. Mounted here, not
	// by either half, so it sits behind the same guard and neither owns it.
	r.StaticFS(sharedweb.StaticPath, http.FS(sharedStatic()))
	todo.Mount(r)
	notes.Mount(r)
	for _, m := range extra {
		m.Mount(r)
	}
	return r
}

// sharedStatic is the shared bundle's static/ directory. The sub-tree always
// exists in the embedded FS, so a failure here is a build defect.
func sharedStatic() fs.FS {
	sub, err := fs.Sub(sharedweb.FS, "static")
	if err != nil {
		panic("server: shared static files missing: " + err.Error())
	}
	return sub
}
