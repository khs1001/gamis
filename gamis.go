// Package gamis is a pure rendering engine that turns AMIS JSON templates
// (JSON files embedding Go template expressions) into final AMIS page JSON.
//
// It does not ship an HTTP layer: callers wire the rendered JSON into their own
// handlers and serve it to an existing AMIS frontend SPA.
package gamis

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"text/template"
)

// Engine renders JSON templates loaded from an fs.FS (os.DirFS or embed.FS).
// Templates are parsed once and cached; an Engine is safe for concurrent use.
type Engine struct {
	fsys    fs.FS
	funcs   template.FuncMap
	compact bool

	cache sync.Map // name -> any (parsed JSON tree)
}

// Option configures an Engine.
type Option func(*Engine)

// New creates an Engine. Configure it with WithFS and WithFuncs.
func New(opts ...Option) *Engine {
	e := &Engine{funcs: template.FuncMap{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithFS sets the filesystem the templates are loaded from.
// Both os.DirFS and embed.FS implement fs.FS and are supported.
func WithFS(fsys fs.FS) Option {
	return func(e *Engine) { e.fsys = fsys }
}

// WithFuncs registers custom template functions. They may override built-ins
// (include, default).
func WithFuncs(f template.FuncMap) Option {
	return func(e *Engine) {
		if e.funcs == nil {
			e.funcs = template.FuncMap{}
		}
		for k, v := range f {
			e.funcs[k] = v
		}
	}
}

// WithCompact switches the output from indented JSON to compact JSON.
func WithCompact() Option {
	return func(e *Engine) { e.compact = true }
}

// Render renders the named template with data and returns the final JSON bytes.
// The template is loaded lazily from the configured fs.FS and cached afterwards.
// The output is validated to be valid JSON before being returned.
func (e *Engine) Render(name string, data any) ([]byte, error) {
	if e.fsys == nil {
		return nil, errors.New("gamis: no filesystem configured; use WithFS")
	}
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("gamis: invalid template name %q", name)
	}
	rv, err := e.render(name, data)
	if err != nil {
		return nil, err
	}
	out, err := e.marshal(rv)
	if err != nil {
		return nil, fmt.Errorf("gamis: marshal rendered %q: %w", name, err)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("gamis: rendered %q is not valid JSON", name)
	}
	return out, nil
}

// Parse pre-parses every ".json" template under the configured fs.FS and
// reports the first template that fails to load or parse. Rendering works
// without calling Parse; it is provided for validation and cache pre-warming.
func (e *Engine) Parse() error {
	if e.fsys == nil {
		return errors.New("gamis: no filesystem configured; use WithFS")
	}
	return fs.WalkDir(e.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".json") {
			if _, err := e.load(p); err != nil {
				return err
			}
		}
		return nil
	})
}

// load reads, decodes and caches a template. Decoding uses json.Decoder with
// UseNumber so that numeric literals in templates are preserved verbatim.
func (e *Engine) load(name string) (any, error) {
	if v, ok := e.cache.Load(name); ok {
		return v, nil
	}
	f, err := e.fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("gamis: open template %q: %w", name, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("gamis: parse template %q: %w", name, err)
	}
	e.cache.Store(name, root)
	return root, nil
}

func (e *Engine) marshal(v any) ([]byte, error) {
	if e.compact {
		return json.Marshal(v)
	}
	return json.MarshalIndent(v, "", "  ")
}
