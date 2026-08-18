package gamis_test

import (
	"fmt"
	"testing/fstest"

	"github.com/khs1001/gamis"
)

func Example() {
	fsys := fstest.MapFS{
		"pages/index.json": &fstest.MapFile{Data: []byte(`{
			"type": "page",
			"title": "{{ .Title }}",
			"body": "{{ include \"header.json\" }}"
		}`)},
		"pages/header.json": &fstest.MapFile{Data: []byte(`{
			"type": "tpl",
			"tpl": "hello {{ .Name }}"
		}`)},
	}

	eng := gamis.New(gamis.WithFS(fsys))

	out, err := eng.Render("pages/index.json", map[string]any{
		"Title": "Hello AMIS",
		"Name":  "world",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))

	// Output:
	// {
	//   "body": {
	//     "tpl": "hello world",
	//     "type": "tpl"
	//   },
	//   "title": "Hello AMIS",
	//   "type": "page"
	// }
}
