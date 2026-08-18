// Command basic demonstrates rendering AMIS JSON templates loaded from a disk
// directory (os.DirFS) and serving the result to a frontend SPA.
//
// Run from this directory: go run .
// Then open http://localhost:8080/api/page
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/khs1001/gamis"
)

func main() {
	eng := gamis.New(gamis.WithFS(os.DirFS("templates")))

	http.HandleFunc("/api/page", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Render("index.json", map[string]any{
			"Title": "欢迎使用 gamis",
			"Name":  "world",
			"Items": []map[string]any{{"name": "A"}, {"name": "B"}, {"name": "C"}},
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
