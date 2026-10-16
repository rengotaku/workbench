// Package sharedweb holds what the todo and note screens have in common: the
// page layout with its site header, the design tokens, the base styles and the
// small script both pages' code builds on. The server serves static/ at
// /shared.
package sharedweb

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
)

//go:embed templates static
var FS embed.FS

// StaticPath is where the server mounts the static bundle.
const StaticPath = "/shared"

// Parse builds the template set for one page: the shared layout and the page
// itself. Every screen draws itself in the browser (router.js), so a page only
// defines its (empty) content; the layout gives it the screens' roots, styles
// and scripts.
func Parse(tool fs.FS, page string) (*template.Template, error) {
	t, err := template.ParseFS(FS, "templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("shared layout: %w", err)
	}
	if _, err := t.ParseFS(tool, "templates/"+page); err != nil {
		return nil, fmt.Errorf("page %s: %w", page, err)
	}
	return t, nil
}
