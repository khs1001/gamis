package gamis

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"text/template"
)

// templateFuncs returns the function map used for expression evaluation:
// the built-ins first, then any user-registered functions (which may override
// the built-ins).
func (rc *renderContext) templateFuncs() template.FuncMap {
	fm := template.FuncMap{
		"include": rc.include,
		"default": defaultFunc,
		"json":    toJSON,
	}
	for k, v := range rc.engine.funcs {
		fm[k] = v
	}
	return fm
}

// toJSON implements the "json" function. It marshals a value to JSON text so
// it can be injected as raw JSON in a whole-value expression. This is the way
// to output maps, slices, structs and any string that happens to look like a
// JSON literal (e.g. {{ json .Name }} for a string "2026").
func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("gamis: json: %w", err)
	}
	return string(b), nil
}

// include is the built-in "include" function. It renders another template from
// the same fs.FS with the same data and returns its JSON text. The included
// template's own whole-value expressions are resolved, so the result injects
// as raw JSON when used as a whole-value expression.
//
// Paths are resolved relative to the including template's directory; absolute
// paths are resolved from the fs.FS root. Cycles are detected and reported.
func (rc *renderContext) include(name string) (string, error) {
	frag := name
	if !path.IsAbs(frag) {
		frag = path.Join(rc.baseDir, frag)
	}
	if !fs.ValidPath(frag) {
		return "", fmt.Errorf("gamis: invalid include path %q", name)
	}
	for _, s := range rc.stack {
		if s == frag {
			return "", fmt.Errorf("gamis: include cycle detected: %s", strings.Join(appendStack(rc.stack, frag), " -> "))
		}
	}

	root, err := rc.engine.load(frag)
	if err != nil {
		return "", err
	}
	child := &renderContext{
		engine:  rc.engine,
		data:    rc.data,
		baseDir: dirOf(frag),
		stack:   appendStack(rc.stack, frag),
	}
	rv, err := child.renderValue(root)
	if err != nil {
		return "", fmt.Errorf("gamis: include %q: %w", frag, err)
	}
	if rv == omitValue {
		return "", nil
	}
	b, err := rc.engine.marshal(rv)
	if err != nil {
		return "", fmt.Errorf("gamis: marshal included %q: %w", frag, err)
	}
	return string(b), nil
}

// defaultFunc implements the "default" function with Helm semantics:
// default defval val returns defval when val is the zero/empty value of its
// type, otherwise val. Usable as {{ default "fallback" .X }} or piped as
// {{ .X | default "fallback" }}.
func defaultFunc(defval, val any) any {
	if isEmptyValue(val) {
		return defval
	}
	return val
}

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Array, reflect.Slice, reflect.Map:
		return rv.Len() == 0
	case reflect.Interface, reflect.Ptr:
		return rv.IsNil()
	}
	return false
}

// appendStack returns a copy of stack with s appended, never mutating the
// caller's backing array.
func appendStack(stack []string, s string) []string {
	out := make([]string, len(stack)+1)
	copy(out, stack)
	out[len(stack)] = s
	return out
}
