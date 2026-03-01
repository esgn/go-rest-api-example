// This file contains all type definitions for the service package:
// sentinel errors, configuration, the domain model, pagination types,
// and the store interface.
//
// Keeping types in a dedicated file mirrors the same convention used in
// internal/model/note.go: each layer owns a types file so definitions
// are easy to locate without scrolling through business logic.
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ── Sentinel errors ──────────────────────────────────────────────────────────
// These are predefined error values that callers can check with errors.Is().
// The handler layer uses them to decide which HTTP status code to return:
//   - ErrInvalidContent  → 400 Bad Request
//   - ErrContentTooLong  → 400 Bad Request
//   - ErrNoteNotFound    → 404 Not Found
//   - ErrInvalidCursor   → 400 Bad Request
//   - ErrInvalidSort     → 400 Bad Request
//   - ErrInvalidLimit    → 400 Bad Request
//   - anything else      → 500 Internal Server Error
var (
	ErrInvalidContent = errors.New("content must not be empty")
	ErrContentTooLong = errors.New("content exceeds maximum length")
	ErrNoteNotFound   = errors.New("note not found")
	ErrInvalidCursor  = errors.New("invalid cursor")
	ErrInvalidSort    = errors.New("invalid sort parameter")
	ErrInvalidLimit   = errors.New("invalid limit parameter")
)

// ── Configuration ─────────────────────────────────────────────────────────────
// Default business rule values used by DefaultConfig(). They define sensible
// defaults that can be overridden via environment variables in main.go.
const (
	defaultMaxContentLength = 10000 // Max characters in a note's content
	defaultMaxTitleLength   = 60    // Max characters for the auto-derived title
	defaultPageLimit        = 20    // Default number of results per page
	defaultMaxPageLimit     = 100   // Maximum allowed results per page
)

// Config holds configurable business rule limits for the service layer.
// These values are BUSINESS decisions (not HTTP or storage decisions), so
// they live in the service package. Their concrete values are provided by
// main.go from environment variables, falling back to DefaultConfig().
type Config struct {
	MaxContentLength int // Max characters allowed in a note's content
	MaxTitleLength   int // Max characters for the auto-derived title
	DefaultPageLimit int // Default number of results per page when not specified
	MaxPageLimit     int // Maximum allowed results per page (hard cap)
}

// DefaultConfig returns a Config populated with the default business rule values.
// Callers can start from this and override individual fields.
func DefaultConfig() Config {
	return Config{
		MaxContentLength: defaultMaxContentLength,
		MaxTitleLength:   defaultMaxTitleLength,
		DefaultPageLimit: defaultPageLimit,
		MaxPageLimit:     defaultMaxPageLimit,
	}
}

// ValidateConfig checks whether service configuration values are valid.
// Call this at startup after environment overrides to fail fast on bad config.
func ValidateConfig(cfg Config) error {
	if cfg.MaxContentLength <= 0 {
		return fmt.Errorf("invalid MaxContentLength: must be > 0")
	}
	if cfg.MaxTitleLength <= 0 {
		return fmt.Errorf("invalid MaxTitleLength: must be > 0")
	}
	if cfg.DefaultPageLimit <= 0 {
		return fmt.Errorf("invalid DefaultPageLimit: must be > 0")
	}
	if cfg.MaxPageLimit <= 0 {
		return fmt.Errorf("invalid MaxPageLimit: must be > 0")
	}
	if cfg.DefaultPageLimit > cfg.MaxPageLimit {
		return fmt.Errorf("invalid page limits: DefaultPageLimit must be <= MaxPageLimit")
	}
	return nil
}

// ── Domain model ─────────────────────────────────────────────────────────────

// Note is the DOMAIN MODEL — the service layer's own representation of a note.
//
// Notice it has NO tags (no `json:"..."`, no `gorm:"..."`). It's a plain Go
// struct. This is intentional:
//   - gen.Note (in the gen package) has JSON tags for the HTTP API
//   - model.Note (in the model package) has GORM tags for the database
//   - service.Note is tag-free because the service doesn't care about
//     serialization or storage — it only cares about business logic.
type Note struct {
	ID        int
	Content   string
	Title     string    // Auto-derived from first line of content
	WordCount int       // Auto-computed from content
	CreatedAt time.Time // Set once at creation
	UpdatedAt time.Time // Updated on every modification
}

// ── Sort order ────────────────────────────────────────────────────────────────

// SortOrder represents a validated sort direction for listing notes.
type SortOrder string

const (
	SortIDAsc         SortOrder = "id"         // Ascending by primary key (default)
	SortIDDesc        SortOrder = "-id"        // Descending by primary key
	SortCreatedAtAsc  SortOrder = "createdAt"  // Oldest first
	SortCreatedAtDesc SortOrder = "-createdAt" // Newest first
)

// ParseSortOrder converts a raw query string into a validated SortOrder.
// Returns SortIDAsc as the default when the input is empty.
func ParseSortOrder(raw string) (SortOrder, error) {
	switch raw {
	case "", "id":
		return SortIDAsc, nil
	case "-id":
		return SortIDDesc, nil
	case "createdAt":
		return SortCreatedAtAsc, nil
	case "-createdAt":
		return SortCreatedAtDesc, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSort, raw)
	}
}

// ── Opaque cursor ─────────────────────────────────────────────────────────────
// The cursor is a base64-encoded JSON object containing the sort column value
// and the tiebreaker ID. This allows stable, resumable pagination regardless
// of sort order. Clients treat it as an opaque string — they never parse it.

// Cursor holds the position of the last item on a page.
// It encodes both the sort-column value (CreatedAt) and the tiebreaker (ID).
type Cursor struct {
	ID        int       `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// EncodeCursor serializes a cursor into an opaque, URL-safe base64 string.
func EncodeCursor(c Cursor) string {
	data, _ := json.Marshal(c) // Cursor is always serializable
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor deserializes an opaque cursor string back into a Cursor.
// Returns ErrInvalidCursor if the string is malformed.
func DecodeCursor(s string) (Cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	return c, nil
}

// ── Pagination ────────────────────────────────────────────────────────────────

// ListParams holds cursor-based pagination parameters.
// After is an optional decoded cursor; nil means "start from the beginning".
// Sort determines which column (and direction) to order by.
// Limit caps the number of results (clamped to [1, MaxPageLimit]).
type ListParams struct {
	After *Cursor   // Position after which to resume (nil = first page)
	Sort  SortOrder // Column + direction to order by
	Limit int       // Max results to return
}

// PaginatedNotes wraps a page of notes with pagination metadata.
type PaginatedNotes struct {
	Data       []Note // The notes on this page
	Limit      int    // The limit that was applied
	HasMore    bool   // Whether more notes exist after this page
	NextCursor string // Opaque cursor for the next page (empty when !HasMore)
}

// ── Store interface ───────────────────────────────────────────────────────────

// NotesStore is an INTERFACE that defines what the service needs from its
// data source. The service doesn't care HOW data is stored — it just needs
// these operations.
type NotesStore interface {
	List(ctx context.Context, params ListParams) ([]Note, error)
	GetByID(ctx context.Context, id int) (Note, error)
	Create(ctx context.Context, note Note) (Note, error)
	Update(ctx context.Context, note Note) (Note, error)
}
