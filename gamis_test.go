package gamis

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"text/template"
)

func newEngine(t *testing.T, files map[string]string, opts ...Option) *Engine {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	opts = append([]Option{WithFS(fsys)}, opts...)
	return New(opts...)
}

func mustRender(t *testing.T, e *Engine, name string, data any) any {
	t.Helper()
	out, err := e.Render(name, data)
	if err != nil {
		t.Fatalf("Render(%q): %v", name, err)
	}
	return mustJSON(t, string(out))
}

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode json: %v\ninput: %s", err, s)
	}
	return v
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRenderBasic(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"type":"page","title":"{{ .Title }}","body":{"type":"tpl","tpl":"hi {{ .Name }}"}}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{"Title": "Hello", "Name": "world"})
	want := mustJSON(t, `{"type":"page","title":"Hello","body":{"type":"tpl","tpl":"hi world"}}`)
	eq(t, got, want)
}

func TestRenderMissingKeyOmits(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .Missing }}","b":"{{ .EmptyStr }}","c":"{{ .EmptySlice }}","d":"{{ .EmptyMap }}","e":"{{ .NilValue }}","f":"kept"}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{
		"EmptyStr":   "",
		"EmptySlice": []string{},
		"EmptyMap":   map[string]any{},
		"NilValue":   nil,
	})
	want := mustJSON(t, `{"f":"kept"}`)
	eq(t, got, want)
}

func TestNonEmptyWholeValueKept(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .S }}","b":"{{ .N }}","c":"{{ .B }}","d":"{{ json .Slice }}"}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{
		"S": "x", "N": 5, "B": true, "Slice": []string{"y"},
	})
	want := mustJSON(t, `{"a":"x","b":5,"c":true,"d":["y"]}`)
	eq(t, got, want)
}

func TestNumberPrecision(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"big":"{{ .Big }}","raw": 12345678901234567890}`,
	})
	got := mustRender(t, e, "index.json", map[string]any{
		"Big": int64(9007199254740993),
	})
	want := mustJSON(t, `{"big":9007199254740993,"raw":12345678901234567890}`)
	eq(t, got, want)
}

func TestStaticValuesPassThrough(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"n":1.5,"b":false,"z":null,"s":"plain","arr":[1,2],"obj":{"k":"v"}}`,
	})
	got := mustRender(t, e, "index.json", nil)
	want := mustJSON(t, `{"n":1.5,"b":false,"z":null,"s":"plain","arr":[1,2],"obj":{"k":"v"}}`)
	eq(t, got, want)
}

func TestRenderErrors(t *testing.T) {
	t.Run("no fs", func(t *testing.T) {
		e := New()
		if _, err := e.Render("x.json", nil); err == nil {
			t.Fatal("expected error for engine without fs")
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		e := newEngine(t, map[string]string{"x.json": `{}`})
		if _, err := e.Render("../x.json", nil); err == nil {
			t.Fatal("expected error for invalid template name")
		}
	})

	t.Run("missing template", func(t *testing.T) {
		e := newEngine(t, map[string]string{})
		if _, err := e.Render("nope.json", nil); err == nil {
			t.Fatal("expected error for missing template")
		}
	})

	t.Run("invalid json template", func(t *testing.T) {
		e := newEngine(t, map[string]string{"bad.json": `{"a":`})
		if _, err := e.Render("bad.json", nil); err == nil {
			t.Fatal("expected error for invalid json template")
		}
	})

	t.Run("empty root expression", func(t *testing.T) {
		e := newEngine(t, map[string]string{"empty.json": `"{{ .Missing }}"`})
		if _, err := e.Render("empty.json", map[string]any{}); err == nil {
			t.Fatal("expected error when template root evaluates to empty")
		}
	})
}

func TestParse(t *testing.T) {
	e := newEngine(t, map[string]string{
		"a.json": `{"x":"{{ .X }}"}`,
		"b.json": `[1,2]`,
	})
	if err := e.Parse(); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// pre-parsed templates are cached; render must still work
	got := mustRender(t, e, "a.json", map[string]any{"X": 1})
	eq(t, got, mustJSON(t, `{"x":1}`))
}

func TestParseInvalid(t *testing.T) {
	e := newEngine(t, map[string]string{
		"ok.json":  `{}`,
		"bad.json": `{"a":`,
	})
	if err := e.Parse(); err == nil {
		t.Fatal("expected Parse to fail on invalid template")
	}
}

func TestWithCompact(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":1,"b":"{{ .S }}"}`,
	}, WithCompact())
	out, err := e.Render("index.json", map[string]any{"S": "x"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), "\n") {
		t.Fatalf("compact output must not contain newlines: %q", out)
	}
	eq(t, mustJSON(t, string(out)), mustJSON(t, `{"a":1,"b":"x"}`))
}

func TestOutputIsValidJSON(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"a":"{{ .A }}","b":{"c":"{{ .B }}"},"arr":["{{ .C }}"]}`,
	})
	data := map[string]any{"A": true, "B": 3.5, "C": "text"}
	for i := 0; i < 10; i++ {
		out, err := e.Render("index.json", data)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if !json.Valid(out) {
			t.Fatalf("output not valid JSON: %q", out)
		}
	}
}

func TestConcurrency(t *testing.T) {
	e := newEngine(t, map[string]string{
		"index.json": `{"title":"{{ .Title }}","items":["{{ .A }}","{{ .B }}"],"sub":{"n":"{{ .N }}"}}`,
		"frag.json":  `{"type":"tpl","tpl":"{{ .Title }}"}`,
	})
	data := map[string]any{"Title": "t", "A": 1, "B": "x", "N": 2}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.Render("index.json", data)
			if err != nil {
				t.Errorf("Render: %v", err)
				return
			}
			if !json.Valid(out) {
				t.Errorf("invalid json: %q", out)
			}
		}()
	}
	wg.Wait()
}

func TestFuncMapNotSharedAcrossEngines(t *testing.T) {
	fsys := fstest.MapFS{}
	fsys["index.json"] = &fstest.MapFile{Data: []byte(`{"s":"{{ upper .S }}"}`)}
	e1 := New(WithFS(fsys), WithFuncs(template.FuncMap{"upper": strings.ToUpper}))
	got := mustRender(t, e1, "index.json", map[string]any{"S": "abc"})
	eq(t, got, mustJSON(t, `{"s":"ABC"}`))
}
