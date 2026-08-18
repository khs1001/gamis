package gamis

import (
	"strings"
	"testing"
)

func TestWholeValueRawTypes(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{
			"b": "{{ .B }}",
			"i": "{{ .I }}",
			"f": "{{ .F }}",
			"s": "{{ .S }}",
			"obj": "{{ json .Obj }}",
			"arr": "{{ json .Arr }}",
			"str": "{{ .Str }}",
			"esc": "{{ json .Esc }}"
		}`,
	})
	data := map[string]any{
		"B":   true,
		"I":   42,
		"F":   3.14,
		"S":   "plain",
		"Obj": map[string]any{"k": "v"},
		"Arr": []any{1, 2, 3},
		"Str": `"quoted"`,
		"Esc": "2026",
	}
	got := mustRender(t, e, "index.json", data)
	want := mustJSON(t, `{
		"b": true,
		"i": 42,
		"f": 3.14,
		"s": "plain",
		"obj": {"k": "v"},
		"arr": [1, 2, 3],
		"str": "quoted",
		"esc": "2026"
	}`)
	eq(t, got, want)
}

func TestWholeValueStringCoercion(t *testing.T) {
	// strings that look like JSON literals are coerced to their JSON type;
	// use the json function to force string output
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .A }}","b":"{{ .B }}","c":"{{ .C }}","d":"{{ json .D }}"}`,
	})
	data := map[string]any{
		"A": "2026",
		"B": "true",
		"C": "null",
		"D": "2026",
	}
	got := mustRender(t, e, "index.json", data)
	want := mustJSON(t, `{"a":2026,"b":true,"d":"2026"}`)
	eq(t, got, want)
}

func TestMixedStringInterpolation(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"title":"Welcome, {{ .Name }}!","noop":"has {{ .Brace }} inside"}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{
		"Name":  "Alice",
		"Brace": "{literal}",
	})
	want := mustJSON(t, `{"title":"Welcome, Alice!","noop":"has {literal} inside"}`)
	eq(t, got, want)
}

func TestWholeValueStringMatchingJSONShape(t *testing.T) {
	// a data value that is itself valid JSON text is injected as raw JSON;
	// a non-JSON string falls back to a plain string
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .A }}","b":"{{ .B }}"}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{
		"A": `{"x": 1}`,
		"B": `not json at all`,
	})
	want := mustJSON(t, `{"a":{"x":1},"b":"not json at all"}`)
	eq(t, got, want)
}

func TestInclude(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json":  `{"type":"page","body":"{{ include \"header.json\" }}","n":"{{ include \"num.json\" }}"}`,
		"header.json": `{"type":"tpl","tpl":"hello {{ .Name }}"}`,
		"num.json":    `42`,
	})
	got := mustRender(t, e, "index.json", map[string]any{"Name": "world"})
	want := mustJSON(t, `{
		"type":"page",
		"body":{"type":"tpl","tpl":"hello world"},
		"n":42
	}`)
	eq(t, got, want)
}

func TestIncludeRelativePath(t *testing.T) {
	e := newEngine(t, map[string]string{
		"pages/index.json":        `{"body":"{{ include \"frags/header.json\" }}","up":"{{ include \"../shared.json\" }}"}`,
		"pages/frags/header.json": `{"tpl":"{{ .Name }}"}`,
		"shared.json":             `{"shared":true}`,
	})
	got := mustRender(t, e, "pages/index.json", map[string]any{"Name": "n"})
	want := mustJSON(t, `{
		"body":{"tpl":"n"},
		"up":{"shared":true}
	}`)
	eq(t, got, want)
}

func TestIncludeNested(t *testing.T) {
	e := newEngine(t, map[string]string{
		"a.json": `{"x":"{{ include \"b.json\" }}"}`,
		"b.json": `{"y":"{{ include \"c.json\" }}"}`,
		"c.json": `{"z":"{{ .V }}"}`,
	})
	got := mustRender(t, e, "a.json", map[string]any{"V": 7})
	want := mustJSON(t, `{"x":{"y":{"z":7}}}`)
	eq(t, got, want)
}

func TestIncludeCycle(t *testing.T) {
	e := newEngine(t, map[string]string{
		"a.json": `{"x":"{{ include \"b.json\" }}"}`,
		"b.json": `{"y":"{{ include \"a.json\" }}"}`,
	})
	if _, err := e.Render("a.json", nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected include cycle error, got: %v", err)
	}
}

func TestIncludeSelfCycle(t *testing.T) {
	e := newEngine(t, map[string]string{
		"a.json": `{"x":"{{ include \"a.json\" }}"}`,
	})
	if _, err := e.Render("a.json", nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected self include cycle error, got: %v", err)
	}
}

func TestIncludeEmptyFragmentOmitsKey(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"keep":"{{ include \"empty.json\" }}","x":"y"}`,
		"empty.json": `"{{ .Missing }}"`,
	})
	got := mustRender(t, e, "index.json", map[string]any{})
	want := mustJSON(t, `{"x":"y"}`)
	eq(t, got, want)
}

func TestIncludeInvalidPath(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"x":"{{ include \"/abs.json\" }}"}`,
	})
	if _, err := e.Render("index.json", nil); err == nil {
		t.Fatal("expected error for absolute include path")
	}
}

func TestIncludeMissing(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"x":"{{ include \"nope.json\" }}"}`,
	})
	if _, err := e.Render("index.json", nil); err == nil {
		t.Fatal("expected error for missing include target")
	}
}
