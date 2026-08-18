// Package gamis is a pure rendering engine that turns AMIS JSON templates
// (JSON files embedding Go template expressions) into final AMIS page JSON.
//
// It does not ship an HTTP layer: callers wire the rendered JSON into their own
// handlers and serve it to an existing AMIS frontend SPA.
package gamis

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// mount binds an fs.FS (os.DirFS, embed.FS, ...) to a path prefix in the
// engine's template namespace.
type mount struct {
	prefix string
	fsys   fs.FS
}

// Engine renders JSON templates loaded from one or more fs.FS sources.
// Each source is mounted under a path prefix and templates are addressed by
// their full names ("pages/index.json"). Templates are parsed once and cached;
// an Engine is safe for concurrent use. Filesystems can be added at runtime
// with AddFS.
type Engine struct {
	funcs   template.FuncMap
	compact bool

	mu     sync.RWMutex
	mounts []mount // sorted by prefix length, longest first

	cache sync.Map // name -> any (parsed JSON tree)
}

// Option configures an Engine.
type Option func(*Engine)

// New creates an Engine. Configure it with WithFS, WithFSAt and WithFuncs.
func New(opts ...Option) *Engine {
	e := &Engine{funcs: template.FuncMap{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithFS mounts a filesystem at the namespace root. Both os.DirFS and embed.FS
// implement fs.FS and are supported. Calling WithFS twice replaces the root
// mount. Use WithFSAt to mount additional filesystems under a prefix.
func WithFS(fsys fs.FS) Option {
	return WithFSAt("", fsys)
}

// WithFSAt mounts a filesystem under the given path prefix. Templates from it
// are addressed as "<prefix>/<file>", e.g. WithFSAt("pages", os.DirFS("pages"))
// makes "pages/index.json" available. An empty prefix mounts at the root and
// acts as the fallback for names that match no other mount. Re-registering the
// same prefix replaces its filesystem.
func WithFSAt(prefix string, fsys fs.FS) Option {
	return func(e *Engine) {
		e.setMount(prefix, fsys)
	}
}

// WithFuncs registers custom template functions. They may override built-ins
// (include, default, json).
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

// AddFS mounts a filesystem under a prefix at runtime. It is safe to call
// concurrently with Render. Re-registering an existing prefix replaces the
// filesystem and invalidates templates cached under that prefix.
func (e *Engine) AddFS(prefix string, fsys fs.FS) {
	e.setMount(prefix, fsys)
}

// Render renders the named template with data and returns the final JSON bytes.
// The template is loaded lazily from the filesystem mounted for its prefix and
// cached afterwards. The output is validated to be valid JSON before being
// returned.
func (e *Engine) Render(name string, data any) ([]byte, error) {
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

// Parse pre-parses every ".json" template in every mounted filesystem and
// reports the first template that fails to load or parse. Rendering works
// without calling Parse; it is provided for validation and cache pre-warming.
func (e *Engine) Parse() error {
	e.mu.RLock()
	mounts := append([]mount(nil), e.mounts...)
	e.mu.RUnlock()

	for _, m := range mounts {
		if err := e.parseFS(m); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) parseFS(m mount) error {
	return fs.WalkDir(m.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".json") {
			name := joinName(m.prefix, p)
			if _, err := e.load(name); err != nil {
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
	fsys, rest, err := e.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := fsys.Open(rest)
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

// resolve maps a template name to the mounted filesystem and the file path
// within it. The longest matching prefix wins; the root mount (prefix "") is
// the fallback, so it is checked last.
func (e *Engine) resolve(name string) (fs.FS, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, m := range e.mounts {
		if m.prefix == "" {
			return m.fsys, name, nil
		}
		if name == m.prefix {
			return m.fsys, ".", nil
		}
		if strings.HasPrefix(name, m.prefix+"/") {
			return m.fsys, name[len(m.prefix)+1:], nil
		}
	}
	return nil, "", fmt.Errorf("gamis: no filesystem mounted for template %q", name)
}

func (e *Engine) setMount(prefix string, fsys fs.FS) {
	if fsys == nil {
		panic("gamis: nil filesystem")
	}
	prefix = normalizePrefix(prefix)

	e.mu.Lock()
	defer e.mu.Unlock()

	replaced := false
	out := e.mounts[:0]
	for _, m := range e.mounts {
		if m.prefix == prefix {
			replaced = true
			continue
		}
		out = append(out, m)
	}
	e.mounts = append(out, mount{prefix: prefix, fsys: fsys})
	sort.Slice(e.mounts, func(i, j int) bool {
		return len(e.mounts[i].prefix) > len(e.mounts[j].prefix)
	})

	if replaced {
		e.evictPrefix(prefix)
	}
}

// evictPrefix drops cached templates that live under the given mount prefix so
// a replaced filesystem takes effect on the next render. The root prefix (""),
// being the fallback for every name not covered by a more specific mount,
// clears the whole cache (other mounts reload their own templates on demand).
func (e *Engine) evictPrefix(prefix string) {
	if prefix == "" {
		e.cache.Range(func(k, _ any) bool {
			e.cache.Delete(k)
			return true
		})
		return
	}
	e.cache.Range(func(k, _ any) bool {
		name := k.(string)
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			e.cache.Delete(name)
		}
		return true
	})
}

// normalizePrefix cleans and validates a mount prefix. The empty string is the
// root prefix. Invalid prefixes panic.
func normalizePrefix(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return ""
	}
	prefix = path.Clean(prefix)
	if !fs.ValidPath(prefix) {
		panic(fmt.Sprintf("gamis: invalid mount prefix %q", prefix))
	}
	return prefix
}

// joinName joins a mount prefix and a file path back into a template name.
func joinName(prefix, file string) string {
	if prefix == "" {
		return file
	}
	return prefix + "/" + file
}

func (e *Engine) marshal(v any) ([]byte, error) {
	if e.compact {
		return json.Marshal(v)
	}
	return json.MarshalIndent(v, "", "  ")
}
