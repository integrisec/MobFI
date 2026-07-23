// Package render turns native Android/iOS file types (XML, plist, web
// caches, config, proprietary formats) into a human-readable view.
// Renderers are pluggable and selected per file.
package render

import (
	"context"
	"errors"
)

// View is a rendered, human-readable representation of a file.
type View struct {
	MIME string // best-guess content type
	Text string // rendered text form
}

// Renderer produces a View for files it recognises.
type Renderer interface {
	// Handles reports whether this renderer recognises the file at path.
	Handles(path string) bool
	// Render produces a human-readable view of the file.
	Render(ctx context.Context, path string) (*View, error)
}

// Registry selects a renderer for a file.
type Registry struct {
	renderers []Renderer
}

// DefaultRegistry returns the registry with all built-in renderers.
// TODO: register XML, plist, SQLite-summary and web-cache renderers.
func DefaultRegistry() *Registry { return &Registry{} }

// Add registers a renderer.
func (r *Registry) Add(rr Renderer) { r.renderers = append(r.renderers, rr) }

// Render finds a renderer that handles path and renders it.
func (r *Registry) Render(ctx context.Context, path string) (*View, error) {
	for _, rr := range r.renderers {
		if rr.Handles(path) {
			return rr.Render(ctx, path)
		}
	}
	return nil, ErrNoRenderer
}

// ErrNoRenderer means no registered renderer recognised the file.
var ErrNoRenderer = errors.New("no renderer for file")

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
