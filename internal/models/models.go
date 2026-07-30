package models

import (
	"database/sql"
	"time"
)

// Link represents a shortened URL.
type Link struct {
	Code      string         `json:"code"`
	TargetURL string         `json:"target_url"`
	CreatedAt time.Time      `json:"created_at"`
	ClickCount int64         `json:"click_count"`
	ExpiresAt sql.NullTime   `json:"expires_at"`
}

// Click represents a single redirect event, aggregated by the worker
// into Link.ClickCount.
type Click struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"created_at"`
}
