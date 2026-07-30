// Command worker periodically folds raw click events into link click
// counts. It has no HTTP surface; it just loops.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/store"
)

func main() {
	s, err := store.Open(config.DatabaseURL())
	if err != nil {
		log.Fatalf("worker: connect to db: %v", err)
	}
	defer s.Close()

	interval := 5 * time.Second
	if v := os.Getenv("WORKER_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	processor := store.NewRetryProcessor(s)

	log.Printf("worker: processing clicks every %s", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("worker: shutting down")
			return
		case <-ticker.C:
			n, err := processor.ProcessClicks(ctx)
			if err != nil {
				log.Printf("worker: process clicks: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("worker: aggregated clicks for %d link(s)", n)
			}
		}
	}
}
