// Command web renders a minimal HTML page listing all links.
package main

import (
	"html/template"
	"log"
	"net/http"

	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/store"
)

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html>
<head><title>linkstash</title></head>
<body>
<h1>linkstash</h1>
<table border="1" cellpadding="6">
<tr><th>Code</th><th>Target</th><th>Clicks</th><th>Created</th></tr>
{{range .}}<tr><td>{{.Code}}</td><td>{{.TargetURL}}</td><td>{{.ClickCount}}</td><td>{{.CreatedAt}}</td></tr>
{{end}}
</table>
</body>
</html>`))

func main() {
	s, err := store.Open(config.DatabaseURL())
	if err != nil {
		log.Fatalf("web: connect to db: %v", err)
	}
	defer s.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		links, err := s.ListLinks(r.Context())
		if err != nil {
			log.Printf("web: list links: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pageTmpl.Execute(w, links); err != nil {
			log.Printf("web: render template: %v", err)
		}
	})

	addr := config.Addr("8082")
	log.Printf("web: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
