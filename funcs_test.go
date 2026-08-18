package gamis

import (
	"fmt"
	"strings"
	"testing"
	"text/template"
)

func TestDefaultFunc(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{
			"a": "{{ default \"fallback\" .A }}",
			"b": "{{ default \"fallback\" .B }}",
			"c": "{{ default \"fallback\" .C }}",
			"d": "{{ default \"fallback\" .D }}",
			"e": "{{ .E | default \"piped\" }}"
		}`,
	})
	data := map[string]any{
		"A": "",         // empty -> fallback
		"B": 0,          // zero -> fallback
		"C": false,      // zero -> fallback
		"D": "real",     // non-empty -> value
		"E": []string{}, // empty -> piped fallback
	}
	got := mustRender(t, e, "index.json", data)
	want := mustJSON(t, `{
		"a":"fallback",
		"b":"fallback",
		"c":"fallback",
		"d":"real",
		"e":"piped"
	}`)
	eq(t, got, want)
}

func TestCustomFuncs(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ upper .A }}","b":"{{ join .Items }}","c":"{{ year }}","d":"{{ json .Y }}"}`,
	}, WithFuncs(template.FuncMap{
		"upper": strings.ToUpper,
		"join":  func(items []string) string { return strings.Join(items, "+") },
		"year":  func() string { return "go" },
	}))
	got := mustRender(t, e, "index.json", map[string]any{
		"A":     "abc",
		"Items": []string{"x", "y"},
		"Y":     "2026",
	})
	want := mustJSON(t, `{"a":"ABC","b":"x+y","c":"go","d":"2026"}`)
	eq(t, got, want)
}

func TestCustomFuncOverridesBuiltin(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ default \"x\" .A }}"}`,
	}, WithFuncs(template.FuncMap{
		"default": func(defval, val any) any {
			return fmt.Sprintf("override(%v,%v)", defval, val)
		},
	}))
	got := mustRender(t, e, "index.json", map[string]any{"A": ""})
	want := mustJSON(t, `{"a":"override(x,)"}`)
	eq(t, got, want)
}

func TestUnknownFunctionErrors(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ nosuch .A }}"}`,
	})
	if _, err := e.Render("index.json", map[string]any{}); err == nil {
		t.Fatal("expected error for unknown template function")
	}
}

func TestBadExpressionErrors(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .A.B }}"}`,
	})
	// nested access on nil errors even though data access is lenient
	if _, err := e.Render("index.json", map[string]any{}); err == nil {
		t.Fatal("expected error for nil pointer dereference in expression")
	}
}
