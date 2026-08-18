// Command embed demonstrates rendering AMIS JSON templates embedded into the
// binary at compile time (go:embed) and serving the result to a frontend SPA.
//
// Run from this directory: go run .
// Then open http://localhost:8080/api/page
package main

import (
	"embed"
	"log"
	"net/http"

	"github.com/khs1001/gamis"
)

//go:embed templates
var templatesFS embed.FS

func main() {
	eng := gamis.New(gamis.WithFS(templatesFS))

	http.HandleFunc("/api/page", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Render("templates/index.json", map[string]any{
			"Title": "欢迎使用 gamis (embed)",
			"Name":  "world",
			"Items": []map[string]any{{"name": "A"}, {"name": "B"}},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(out)
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
