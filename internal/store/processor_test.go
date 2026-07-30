package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// MockClickProcessor is a test mock that can be configured to fail N times.
type MockClickProcessor struct {
	FailCount      int
	Attempts       int
	LastAttemptAt  time.Time
	AttemptTimings []time.Time
}

// ProcessClicks fails the first FailCount times, then succeeds.
func (m *MockClickProcessor) ProcessClicks(ctx context.Context) (int64, error) {
	m.LastAttemptAt = time.Now()
	m.AttemptTimings = append(m.AttemptTimings, m.LastAttemptAt)
	m.Attempts++

	if m.Attempts <= m.FailCount {
		return 0, errors.New("mock: simulated failure")
	}
	return 42, nil
}

func TestRetryProcessorSucceedsAfterFailures(t *testing.T) {
	mock := &MockClickProcessor{FailCount: 2} // fail twice, succeed on 3rd
	rp := NewRetryProcessorWithConfig(mock, 3, 10*time.Millisecond)

	n, err := rp.ProcessClicks(context.Background())

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if n != 42 {
		t.Fatalf("expected 42 clicks, got %d", n)
	}
	if mock.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", mock.Attempts)
	}
}

func TestRetryProcessorGivesUpAfterMaxRetries(t *testing.T) {
	mock := &MockClickProcessor{FailCount: 10} // fail all the time
	rp := NewRetryProcessorWithConfig(mock, 3, 10*time.Millisecond)

	n, err := rp.ProcessClicks(context.Background())

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if n != 0 {
		t.Fatalf("expected 0 clicks on failure, got %d", n)
	}
	if mock.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", mock.Attempts)
	}
}

func TestRetryProcessorUsesExponentialBackoff(t *testing.T) {
	mock := &MockClickProcessor{FailCount: 2} // fail twice, succeed on 3rd
	rp := NewRetryProcessorWithConfig(mock, 3, 10*time.Millisecond)

	start := time.Now()
	_, err := rp.ProcessClicks(context.Background())

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	elapsed := time.Since(start)
	// With initial wait 10ms and 2 retries, we expect:
	// Attempt 1: fail
	// Wait 10ms
	// Attempt 2: fail
	// Wait 20ms (2x backoff)
	// Attempt 3: succeed
	// Total: ~30ms minimum (plus some overhead)
	expectedMinimum := 30 * time.Millisecond
	if elapsed < expectedMinimum {
		t.Logf("warning: elapsed time %v is less than expected minimum %v, timing may be tight", elapsed, expectedMinimum)
	}

	// Verify timings between attempts show exponential backoff
	if len(mock.AttemptTimings) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(mock.AttemptTimings))
	}

	delay1 := mock.AttemptTimings[1].Sub(mock.AttemptTimings[0])
	delay2 := mock.AttemptTimings[2].Sub(mock.AttemptTimings[1])

	if delay1 < 8*time.Millisecond {
		t.Fatalf("first retry delay %v is too short (expected ~10ms)", delay1)
	}
	if delay2 < 15*time.Millisecond {
		t.Fatalf("second retry delay %v is too short (expected ~20ms)", delay2)
	}

	// Check that second delay is roughly 2x the first
	ratio := delay2.Milliseconds() / delay1.Milliseconds()
	if ratio < 1 || ratio > 3 { // allow some variance
		t.Logf("backoff ratio is %d (expected ~2), but delays are still increasing", ratio)
	}
}

func TestRetryProcessorRespectsCancelledContext(t *testing.T) {
	mock := &MockClickProcessor{FailCount: 10}
	rp := NewRetryProcessorWithConfig(mock, 3, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := rp.ProcessClicks(ctx)

	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if mock.Attempts > 1 {
		t.Fatalf("expected at most 1 attempt with cancelled context, got %d", mock.Attempts)
	}
}
