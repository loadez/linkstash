package store

import (
	"context"
	"log"
	"time"
)

// ClickProcessor defines the interface for processing clicks.
type ClickProcessor interface {
	ProcessClicks(ctx context.Context) (int64, error)
}

// RetryProcessor wraps a ClickProcessor and retries ProcessClicks with exponential backoff.
type RetryProcessor struct {
	processor   ClickProcessor
	maxRetries  int
	initialWait time.Duration
}

// NewRetryProcessor creates a new retry processor with sensible defaults:
// max 3 retries, starting with 100ms initial wait.
func NewRetryProcessor(processor ClickProcessor) *RetryProcessor {
	return &RetryProcessor{
		processor:   processor,
		maxRetries:  3,
		initialWait: 100 * time.Millisecond,
	}
}

// NewRetryProcessorWithConfig creates a new retry processor with custom settings.
// Useful for testing with shorter delays.
func NewRetryProcessorWithConfig(processor ClickProcessor, maxRetries int, initialWait time.Duration) *RetryProcessor {
	return &RetryProcessor{
		processor:   processor,
		maxRetries:  maxRetries,
		initialWait: initialWait,
	}
}

// ProcessClicks retries the underlying processor with exponential backoff on error.
// Returns the number of clicks processed on success, or error after maxRetries attempts.
func (rp *RetryProcessor) ProcessClicks(ctx context.Context) (int64, error) {
	var lastErr error
	wait := rp.initialWait

	for attempt := 0; attempt < rp.maxRetries; attempt++ {
		n, err := rp.processor.ProcessClicks(ctx)
		if err == nil {
			return n, nil
		}

		lastErr = err
		if attempt < rp.maxRetries-1 {
			log.Printf("worker: process clicks attempt %d failed, retrying in %v: %v", attempt+1, wait, err)
			select {
			case <-time.After(wait):
				wait *= 2 // exponential backoff
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
	}

	log.Printf("worker: process clicks failed after %d attempts: %v", rp.maxRetries, lastErr)
	return 0, lastErr
}
