// Package store implements the persistence layer for linkstash on top of
// database/sql + lib/pq. Kept deliberately small: one file, one struct,
// methods for the operations the services actually need.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	_ "github.com/lib/pq"

	"github.com/loadez/linkstash/internal/models"
)

// ErrNotFound is returned when a code has no matching link.
var ErrNotFound = errors.New("store: link not found")

const codeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Store wraps a *sql.DB with linkstash-specific queries.
type Store struct {
	db *sql.DB
}

// Open connects to Postgres at dsn and verifies the connection with a ping.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(10)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{db: db}, nil
}

// New wraps an already-open *sql.DB (handy for tests).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// GenerateCode returns a random 7-character short code.
func GenerateCode() (string, error) {
	b := make([]byte, 7)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b), nil
}

// CreateLink inserts a new link. If code is empty, one is generated and
// retried on collision.
func (s *Store) CreateLink(ctx context.Context, code, targetURL string) (*models.Link, error) {
	if targetURL == "" {
		return nil, errors.New("store: target_url is required")
	}

	tryInsert := func(c string) (*models.Link, error) {
		row := s.db.QueryRowContext(ctx,
			`INSERT INTO links (code, target_url) VALUES ($1, $2)
			 RETURNING code, target_url, created_at, click_count`,
			c, targetURL)
		return scanLink(row)
	}

	if code != "" {
		return tryInsert(code)
	}

	const maxAttempts = 5
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		c, err := GenerateCode()
		if err != nil {
			return nil, err
		}
		link, err := tryInsert(c)
		if err == nil {
			return link, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("store: create link: %w", lastErr)
}

// GetLink fetches a single link by code.
func (s *Store) GetLink(ctx context.Context, code string) (*models.Link, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT code, target_url, created_at, click_count FROM links WHERE code = $1`, code)
	link, err := scanLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return link, err
}

// ListLinks returns all links ordered by newest first.
func (s *Store) ListLinks(ctx context.Context) ([]models.Link, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code, target_url, created_at, click_count FROM links ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list links: %w", err)
	}
	defer rows.Close()

	var links []models.Link
	for rows.Next() {
		var l models.Link
		if err := rows.Scan(&l.Code, &l.TargetURL, &l.CreatedAt, &l.ClickCount); err != nil {
			return nil, fmt.Errorf("store: scan link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// RecordClick appends a click event for code. Aggregation into
// links.click_count happens asynchronously via ProcessClicks.
func (s *Store) RecordClick(ctx context.Context, code string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO clicks (code, created_at) VALUES ($1, now())`, code)
	if err != nil {
		return fmt.Errorf("store: record click: %w", err)
	}
	return nil
}

// ProcessClicks folds all unprocessed click rows into links.click_count and
// marks them processed. Returns the number of click rows processed. This is
// what the worker service calls on a loop.
func (s *Store) ProcessClicks(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE links l SET click_count = l.click_count + sub.cnt
		FROM (
			SELECT code, count(*) AS cnt
			FROM clicks
			WHERE processed = false
			GROUP BY code
		) sub
		WHERE l.code = sub.code`)
	if err != nil {
		return 0, fmt.Errorf("store: aggregate clicks: %w", err)
	}
	updated, _ := res.RowsAffected()

	if _, err := tx.ExecContext(ctx, `UPDATE clicks SET processed = true WHERE processed = false`); err != nil {
		return 0, fmt.Errorf("store: mark clicks processed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return updated, nil
}

// DeleteLink removes a link and its associated clicks via cascade.
// Returns ErrNotFound if the code does not exist.
func (s *Store) DeleteLink(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM links WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("store: delete link: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func scanLink(row *sql.Row) (*models.Link, error) {
	var l models.Link
	if err := row.Scan(&l.Code, &l.TargetURL, &l.CreatedAt, &l.ClickCount); err != nil {
		return nil, err
	}
	return &l, nil
}
