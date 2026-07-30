// Command web renders a minimal HTML page listing all links.
package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"time"

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

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		logger.InfoContext(r.Context(), "request",
			slog.String("method", r.Method),
			slog.String("path", r.RequestURI),
			slog.Int("status", rw.statusCode),
			slog.Duration("duration", duration),
		)
	})
}

func main() {
	s, err := store.Open(config.DatabaseURL())
	if err != nil {
		slog.Error("web: connect to db", slog.Any("error", err))
		return
	}
	defer s.Close()

	logger := slog.Default()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		links, err := s.ListLinks(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "web: list links", slog.Any("error", err))
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pageTmpl.Execute(w, links); err != nil {
			slog.ErrorContext(r.Context(), "web: render template", slog.Any("error", err))
		}
	})

	addr := config.Addr("8082")
	slog.Info("web: listening", slog.String("addr", addr))

	handler := loggingMiddleware(logger, mux)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("web: listen and serve", slog.Any("error", err))
		return
	}
}
