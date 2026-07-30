package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a JSON handler that writes to the buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(handler)

	// Create a simple test handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with logging middleware
	loggingHandler := loggingMiddleware(logger, nextHandler)

	// Create a test request
	req := httptest.NewRequest("GET", "/test-path", nil)
	rr := httptest.NewRecorder()

	// Call the handler
	loggingHandler.ServeHTTP(rr, req)

	// Parse the log output as JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to unmarshal log JSON: %v", err)
	}

	// Check that required fields are present
	requiredFields := []string{"method", "path", "status", "duration"}
	for _, field := range requiredFields {
		if _, ok := logEntry[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Verify field values
	if method, ok := logEntry["method"].(string); !ok || method != "GET" {
		t.Errorf("Expected method=GET, got %v", logEntry["method"])
	}

	if path, ok := logEntry["path"].(string); !ok || path != "/test-path" {
		t.Errorf("Expected path=/test-path, got %v", logEntry["path"])
	}

	if status, ok := logEntry["status"].(float64); !ok || status != 200 {
		t.Errorf("Expected status=200, got %v", logEntry["status"])
	}

	if _, ok := logEntry["duration"]; !ok {
		t.Errorf("Expected duration field to be present")
	}

	// Verify response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}
