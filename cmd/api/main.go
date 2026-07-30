// Command api serves the HTTP API for creating and listing shortened links.
package main

import (
	"log"
	"net/http"

	"github.com/loadez/linkstash/internal/apisrv"
	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/store"
)

func main() {
	s, err := store.Open(config.DatabaseURL())
	if err != nil {
		log.Fatalf("api: connect to db: %v", err)
	}
	defer s.Close()

	addr := config.Addr("8080")
	log.Printf("api: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, apisrv.NewHandler(s)))
}
