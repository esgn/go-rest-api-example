// Package service contains the business logic of the application.
//
// This is the most important layer architecturally because it:
//   - Defines the DOMAIN MODEL (service.Note) — the "truth" of what a Note is
//   - Contains BUSINESS RULES (validation, derived fields, constraints)
//   - Defines the NotesStore INTERFACE that the repository must implement
//
// KEY DESIGN PRINCIPLE: This package has ZERO external dependencies.
// It doesn't import GORM, HTTP, JSON, or any database package.
// It only uses Go's standard library (context, errors, strings, time).
// This makes it easy to test, easy to understand, and immune to
// infrastructure changes.
//
// ──────────────────────────────────────────────────────────────────────────────
// WHY THIS LAYER MATTERS (the whole point of this example):
//
// Without a service layer, you'd have two bad options:
//  1. Put business logic in HANDLERS → now your HTTP layer knows about word
//     counting, title derivation, max lengths… and you can't reuse that logic
//     in a CLI tool, a gRPC server, or a background job.
//  2. Put business logic in the REPOSITORY → now your data access layer is
//     doing validation and computation, mixing "how to store" with "what the
//     rules are". Good luck testing business rules without a database.
//
// The service layer gives you a single place for business rules that is:
//   - Independent of HTTP (no request/response objects)
//   - Independent of the database (talks through an interface)
//   - Easy to unit test (just mock the interface)
//   - Reusable from any entry point (API, CLI, worker, tests)
//
// Package layout:
//   - types.go        — domain types, errors, config, store interface
//   - notes_service.go — service struct and all use-case methods
//
// ──────────────────────────────────────────────────────────────────────────────
package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// NotesService orchestrates business logic between the HTTP handler
// and the repository via the NotesStore interface.
type NotesService struct {
	store NotesStore // persistence layer
	cfg   Config     // configurable business rule limits
}

// NewNotesService creates a new NotesService with the given store and config.
// Use DefaultConfig() as a starting point and override fields as needed.
func NewNotesService(store NotesStore, cfg Config) *NotesService {
	return &NotesService{store: store, cfg: cfg}
}

// ── Service methods ──────────────────────────────────────────────────────────
// Each method represents a USE CASE of the application.
//
// ListNotes follows a pagination pattern:
//   1. Parse and validate the sort order
//   2. Decode the opaque cursor string (if any)
//   3. Normalise pagination params (apply defaults, clamp to limits)
//   4. Fetch limit+1 records to detect whether a next page exists
//   5. Return a PaginatedNotes envelope (data, total, hasMore, nextCursor)
//
// CreateNote / UpdateNote follow a mutation pattern:
//   1. Validate inputs (business rules)
//   2. Compute derived values (title, word count, timestamps)
//   3. Delegate to the store for persistence
//   4. Return the resulting domain object (service.Note)
//
// The handler calls these methods. The repository implements the store.
// Neither needs to know what happens inside.

// ListNotes returns a paginated list of notes using cursor-based pagination.
// It validates and decodes the opaque cursor string, parses the sort order,
// normalizes the limit, fetches one extra record to determine hasMore, and
// returns a PaginatedNotes envelope with an opaque nextCursor.
func (s *NotesService) ListNotes(ctx context.Context, cursorStr, sort string, limit int) (PaginatedNotes, error) {
	// ── Parse sort order ──
	sortOrder, err := ParseSortOrder(sort)
	if err != nil {
		return PaginatedNotes{}, err
	}

	// ── Decode cursor (if provided) ──
	var cursor *Cursor
	if cursorStr != "" {
		c, err := DecodeCursor(cursorStr)
		if err != nil {
			return PaginatedNotes{}, err
		}
		cursor = &c
	}

	// ── Validate and normalize limit ──
	// 0 means "not provided" (Go zero value when the query param is absent),
	// so we apply the default. Negative values and values above MaxPageLimit
	// are explicit errors that the caller must fix.
	if limit < 0 {
		return PaginatedNotes{}, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, s.cfg.MaxPageLimit)
	}
	if limit == 0 {
		limit = s.cfg.DefaultPageLimit
	}
	if limit > s.cfg.MaxPageLimit {
		return PaginatedNotes{}, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, s.cfg.MaxPageLimit)
	}

	// Fetch one extra to detect whether there are more results.
	params := ListParams{After: cursor, Sort: sortOrder, Limit: limit + 1}
	notes, err := s.store.List(ctx, params)
	if err != nil {
		return PaginatedNotes{}, fmt.Errorf("list notes: %w", err)
	}

	// Count total notes (independent of cursor).
	total, err := s.store.Count(ctx)
	if err != nil {
		return PaginatedNotes{}, fmt.Errorf("count notes: %w", err)
	}

	hasMore := len(notes) > limit
	if hasMore {
		notes = notes[:limit] // Trim the extra record
	}

	var nextCursor string
	if hasMore && len(notes) > 0 {
		last := notes[len(notes)-1]
		nextCursor = EncodeCursor(Cursor{ID: last.ID, CreatedAt: last.CreatedAt})
	}

	return PaginatedNotes{
		Data:       notes,
		Total:      total,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// GetNote retrieves a single note by ID.
// If the note doesn't exist, the repository returns ErrNoteNotFound, which
// we propagate up. The handler then maps this to HTTP 404.
func (s *NotesService) GetNote(ctx context.Context, id int) (Note, error) {
	note, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Note{}, fmt.Errorf("get note: %w", err)
	}
	return note, nil
}

// CreateNote validates the content, computes derived fields, and creates a note.
//
// THIS IS WHERE THE SERVICE LAYER SHINES. Look at everything it does:
//  1. Trims whitespace (normalization)
//  2. Rejects empty content (business rule)
//  3. Enforces max length (business rule)
//  4. Derives a title from the content (business logic)
//  5. Counts words (business logic)
//  6. Sets timestamps (domain concern)
//  7. Delegates to the store for persistence
//
// None of this belongs in the HTTP handler (which only knows about HTTP).
// None of this belongs in the repository (which only knows about SQL).
// It belongs HERE, in the service, because these are BUSINESS DECISIONS.
func (s *NotesService) CreateNote(ctx context.Context, content string) (Note, error) {
	// ── Step 1: Normalize ──
	trimmed := strings.TrimSpace(content)

	// ── Step 2: Validate ──
	if trimmed == "" {
		return Note{}, ErrInvalidContent
	}
	if utf8.RuneCountInString(trimmed) > s.cfg.MaxContentLength {
		return Note{}, fmt.Errorf("%w (%d characters)", ErrContentTooLong, s.cfg.MaxContentLength)
	}

	// ── Step 3: Compute derived fields ──
	// The handler doesn't know about these computations.
	// The repository doesn't either — it just stores what we give it.
	now := time.Now().UTC()
	note := Note{
		Content:   trimmed,
		Title:     s.deriveTitle(trimmed), // Business logic: extract a short title
		WordCount: countWords(trimmed),    // Business logic: count words
		CreatedAt: now,
		UpdatedAt: now,
	}

	// ── Step 4: Persist ──
	created, err := s.store.Create(ctx, note)
	if err != nil {
		return Note{}, fmt.Errorf("create note: %w", err)
	}
	return created, nil
}

// UpdateNote validates the new content, recomputes derived fields, and updates.
//
// ANOTHER GREAT EXAMPLE of why the service layer matters:
//   - It first checks that the note EXISTS (by calling GetByID)
//   - It validates the new content (same rules as create)
//   - It RECOMPUTES title and word count from the new content
//   - It updates UpdatedAt but PRESERVES the original CreatedAt
//
// If this logic were in the handler, you'd be mixing HTTP parsing with
// business rules. If it were in the repository, you'd be mixing SQL with
// validation. The service keeps everything clean.
func (s *NotesService) UpdateNote(ctx context.Context, id int, content string) (Note, error) {
	// ── Step 1: Check existence ──
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Note{}, fmt.Errorf("update note: %w", err)
	}

	// ── Step 2: Validate new content ──
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return Note{}, ErrInvalidContent
	}
	if utf8.RuneCountInString(trimmed) > s.cfg.MaxContentLength {
		return Note{}, fmt.Errorf("%w (%d characters)", ErrContentTooLong, s.cfg.MaxContentLength)
	}

	// ── Step 3: Recompute derived fields ──
	// Title and word count are recomputed because the content changed.
	// CreatedAt is PRESERVED — it was set when the note was first created.
	// UpdatedAt is refreshed to "now".
	existing.Content = trimmed
	existing.Title = s.deriveTitle(trimmed)
	existing.WordCount = countWords(trimmed)
	existing.UpdatedAt = time.Now().UTC()

	// ── Step 4: Persist ──
	updated, err := s.store.Update(ctx, existing)
	if err != nil {
		return Note{}, fmt.Errorf("update note: %w", err)
	}
	return updated, nil
}

// ── Pure business logic functions ────────────────────────────────────────────
// These are plain Go functions with no side effects. They don't talk to the
// database or the network. They just transform data. This makes them trivially
// easy to unit test.

// deriveTitle extracts a short title from the note content.
//
// Algorithm:
//  1. Take the first line of the content
//  2. If it's short enough (≤ cfg.MaxTitleLength), use it as-is
//  3. If it's too long, truncate at the last word boundary and add "…"
//
// Example:
//
//	"Buy milk and eggs" → "Buy milk and eggs"
//	"This is a very long first line that exceeds the maximum title length limit" → "This is a very long first line that exceeds the…"
func (s *NotesService) deriveTitle(content string) string {
	// Take the first line only
	firstLine := content
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		firstLine = content[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)

	// If it fits, use it directly
	if utf8.RuneCountInString(firstLine) <= s.cfg.MaxTitleLength {
		return firstLine
	}

	// Truncate at word boundary to avoid cutting in the middle of a word
	runes := []rune(firstLine)
	truncated := string(runes[:s.cfg.MaxTitleLength])
	if lastSpace := strings.LastIndexByte(truncated, ' '); lastSpace > 0 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "…"
}

// countWords counts the number of whitespace-separated words in the content.
// strings.Fields splits on any whitespace (spaces, tabs, newlines) and ignores
// leading/trailing whitespace, so "  hello   world  " → ["hello", "world"] → 2.
func countWords(content string) int {
	return len(strings.Fields(content))
}
