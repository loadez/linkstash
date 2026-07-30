// Command redirector serves 302 redirects for short codes and records a
// click event for each hit (aggregation happens in the worker service).
package main

import (
	"log"
	"net/http"

	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/redirectsrv"
	"github.com/loadez/linkstash/internal/store"
)

func main() {
	s, err := store.Open(config.DatabaseURL())
	if err != nil {
		log.Fatalf("redirector: connect to db: %v", err)
	}
	defer s.Close()

	addr := config.Addr("8081")
	log.Printf("redirector: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, redirectsrv.NewHandler(s)))
}
