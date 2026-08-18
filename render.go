package gamis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
)

// omitValue marks a rendered value that should be dropped from the output:
// either a whole-value expression that evaluated to empty, or a null/empty
// fragment include result.
var omitValue = &struct{}{}

// renderContext carries the state of a single render pass: the engine, the
// data context, the directory of the template being walked (so include paths
// resolve relative to it) and the include stack used for cycle detection.
type renderContext struct {
	engine  *Engine
	data    any
	baseDir string
	stack   []string
}

// render walks the parsed template tree and returns the rendered value.
func (e *Engine) render(name string, data any) (any, error) {
	root, err := e.load(name)
	if err != nil {
		return nil, err
	}
	rc := &renderContext{
		engine:  e,
		data:    data,
		baseDir: dirOf(name),
		stack:   []string{name},
	}
	rv, err := rc.renderValue(root)
	if err != nil {
		return nil, fmt.Errorf("gamis: render %q: %w", name, err)
	}
	if rv == omitValue {
		return nil, fmt.Errorf("gamis: render %q: template root evaluated to empty", name)
	}
	return rv, nil
}

func (rc *renderContext) renderValue(v any) (any, error) {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, cv := range val {
			rv, err := rc.renderValue(cv)
			if err != nil {
				return nil, err
			}
			if rv == omitValue {
				continue
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(val))
		for _, cv := range val {
			rv, err := rc.renderValue(cv)
			if err != nil {
				return nil, err
			}
			if rv == omitValue {
				continue
			}
			out = append(out, rv)
		}
		return out, nil
	case string:
		return rc.renderString(val)
	default:
		return v, nil
	}
}

// renderString renders a string value. Strings without template delimiters
// pass through unchanged. A string that is exactly one expression is a
// "whole-value expression": its result is injected as raw JSON (bool, number,
// object or array keep their types) or the field is omitted when the result
// is empty. Any other string is rendered as text.
func (rc *renderContext) renderString(s string) (any, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	trimmed := strings.TrimSpace(s)
	if isWholeExpression(trimmed) {
		text, err := rc.evalExpr(trimmed)
		if err != nil {
			return nil, err
		}
		if isEmptyExprResult(text) {
			return omitValue, nil
		}
		if raw, ok := parseRawJSON(text); ok {
			return raw, nil
		}
		return text, nil
	}
	text, err := rc.evalExpr(s)
	if err != nil {
		return nil, err
	}
	return text, nil
}

// isWholeExpression reports whether s is a single template expression spanning
// the whole string, e.g. "{{ .X }}" or "{{ default \"d\" .Y }}".
func isWholeExpression(s string) bool {
	if len(s) < 4 || !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return false
	}
	inner := s[2 : len(s)-2]
	return !strings.Contains(inner, "{{") && !strings.Contains(inner, "}}")
}

// isEmptyExprResult reports whether a rendered whole-value expression is empty
// and should therefore be omitted. It covers the markers text/template emits
// for missing/zero values plus the JSON/Go renderings of empty collections.
func isEmptyExprResult(text string) bool {
	switch strings.TrimSpace(text) {
	case "", "<no value>", "<nil>", "null", "[]", "{}", "map[]":
		return true
	}
	return false
}

// parseRawJSON parses text as a single JSON value using UseNumber so numeric
// precision is preserved. The bool result reports whether the text is valid,
// single-value JSON.
func parseRawJSON(text string) (any, bool) {
	if !json.Valid([]byte(text)) {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return v, true
}

// evalExpr renders a single template expression against the render context's
// data. Expressions are parsed per evaluation so the include stack and
// per-render function map are always current.
func (rc *renderContext) evalExpr(expr string) (string, error) {
	t, err := template.New("_").
		Option("missingkey=zero").
		Funcs(rc.templateFuncs()).
		Parse(expr)
	if err != nil {
		return "", fmt.Errorf("gamis: parse expression %q: %w", expr, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rc.data); err != nil {
		return "", fmt.Errorf("gamis: execute expression %q: %w", expr, err)
	}
	return buf.String(), nil
}

// dirOf returns the directory part of a template path, "." for top-level files.
func dirOf(name string) string {
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return "."
	}
	if i == 0 {
		return "/"
	}
	return name[:i]
}
