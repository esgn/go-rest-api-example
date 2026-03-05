package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// internalMock is a simple in-package mock for NotesStore, used by service
// method tests that live in package service (so they can also access
// unexported helpers). External packages (for example HTTP handler tests)
// keep their own local mocks in their test package.
type internalMock struct {
	ListFn    func(ctx context.Context, params ListParams) ([]Note, error)
	GetByIDFn func(ctx context.Context, id int) (Note, error)
	CreateFn  func(ctx context.Context, note Note) (Note, error)
	UpdateFn  func(ctx context.Context, note Note) (Note, error)
}

func (m *internalMock) List(ctx context.Context, params ListParams) ([]Note, error) {
	return m.ListFn(ctx, params)
}
func (m *internalMock) GetByID(ctx context.Context, id int) (Note, error) {
	return m.GetByIDFn(ctx, id)
}
func (m *internalMock) Create(ctx context.Context, note Note) (Note, error) {
	return m.CreateFn(ctx, note)
}
func (m *internalMock) Update(ctx context.Context, note Note) (Note, error) {
	return m.UpdateFn(ctx, note)
}

// ── ListNotes ────────────────────────────────────────────────────────────────

func TestListNotes_DefaultLimit(t *testing.T) {
	cfg := Config{DefaultPageLimit: 5, MaxPageLimit: 100, MaxTitleLength: 50}
	mock := &internalMock{
		ListFn: func(_ context.Context, p ListParams) ([]Note, error) {
			if p.Limit != 6 { // 5 + 1 extra for hasMore detection
				t.Errorf("List called with limit %d, want 6", p.Limit)
			}
			return []Note{{ID: 1}, {ID: 2}}, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	result, err := svc.ListNotes(context.Background(), "", "", 0) // limit=0 → default
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Limit != 5 {
		t.Errorf("Limit = %d, want 5", result.Limit)
	}
	if result.HasMore {
		t.Error("HasMore should be false when fewer results than limit")
	}
	if len(result.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(result.Data))
	}
}

func TestListNotes_ExplicitLimit(t *testing.T) {
	cfg := Config{DefaultPageLimit: 20, MaxPageLimit: 100, MaxTitleLength: 50}
	mock := &internalMock{
		ListFn: func(_ context.Context, p ListParams) ([]Note, error) {
			if p.Limit != 11 { // 10 + 1
				t.Errorf("List called with limit %d, want 11", p.Limit)
			}
			return make([]Note, 10), nil
		},
	}
	svc := NewNotesService(mock, cfg)

	result, err := svc.ListNotes(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Limit != 10 {
		t.Errorf("Limit = %d, want 10", result.Limit)
	}
}

func TestListNotes_NegativeLimit(t *testing.T) {
	cfg := Config{DefaultPageLimit: 20, MaxPageLimit: 100}
	svc := NewNotesService(nil, cfg) // store never called

	_, err := svc.ListNotes(context.Background(), "", "", -1)
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
	if !errors.Is(err, ErrInvalidLimit) {
		t.Errorf("expected ErrInvalidLimit, got %v", err)
	}
}

func TestListNotes_ExceedsMaxLimit(t *testing.T) {
	cfg := Config{DefaultPageLimit: 20, MaxPageLimit: 100}
	svc := NewNotesService(nil, cfg)

	_, err := svc.ListNotes(context.Background(), "", "", 101)
	if err == nil {
		t.Fatal("expected error for oversized limit")
	}
	if !errors.Is(err, ErrInvalidLimit) {
		t.Errorf("expected ErrInvalidLimit, got %v", err)
	}
}

func TestListNotes_InvalidSort(t *testing.T) {
	svc := NewNotesService(nil, DefaultConfig())

	_, err := svc.ListNotes(context.Background(), "", "invalid", 10)
	if !errors.Is(err, ErrInvalidSort) {
		t.Errorf("expected ErrInvalidSort, got %v", err)
	}
}

func TestListNotes_InvalidCursor(t *testing.T) {
	svc := NewNotesService(nil, DefaultConfig())

	_, err := svc.ListNotes(context.Background(), "!!!bad!!!", "", 10)
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestListNotes_HasMoreAndNextCursor(t *testing.T) {
	cfg := Config{DefaultPageLimit: 2, MaxPageLimit: 100, MaxTitleLength: 50}
	notes := []Note{
		{ID: 1, CreatedAt: fixedTime},
		{ID: 2, CreatedAt: fixedTime.Add(time.Second)},
		{ID: 3, CreatedAt: fixedTime.Add(2 * time.Second)}, // extra record
	}
	mock := &internalMock{
		ListFn: func(_ context.Context, _ ListParams) ([]Note, error) { return notes, nil },
	}
	svc := NewNotesService(mock, cfg)

	result, err := svc.ListNotes(context.Background(), "", "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasMore {
		t.Error("HasMore should be true")
	}
	if len(result.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2 (should trim extra)", len(result.Data))
	}
	if result.NextCursor == "" {
		t.Error("NextCursor should be non-empty when HasMore is true")
	}

	// The cursor should decode back to the last returned note's position.
	cursor, err := DecodeCursor(result.NextCursor)
	if err != nil {
		t.Fatalf("NextCursor decode error: %v", err)
	}
	if cursor.ID != 2 {
		t.Errorf("cursor.ID = %d, want 2", cursor.ID)
	}
}

func TestListNotes_EmptyResults(t *testing.T) {
	cfg := DefaultConfig()
	mock := &internalMock{
		ListFn: func(_ context.Context, _ ListParams) ([]Note, error) { return nil, nil },
	}
	svc := NewNotesService(mock, cfg)

	result, err := svc.ListNotes(context.Background(), "", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0", len(result.Data))
	}
	if result.HasMore {
		t.Error("HasMore should be false for empty results")
	}
	if result.NextCursor != "" {
		t.Errorf("NextCursor should be empty, got %q", result.NextCursor)
	}
}

func TestListNotes_StoreListError(t *testing.T) {
	mock := &internalMock{
		ListFn: func(_ context.Context, _ ListParams) ([]Note, error) {
			return nil, fmt.Errorf("db down")
		},
	}
	svc := NewNotesService(mock, DefaultConfig())

	_, err := svc.ListNotes(context.Background(), "", "", 10)
	if err == nil {
		t.Fatal("expected error when store.List fails")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Errorf("error should wrap store error, got: %v", err)
	}
}

func TestListNotes_WithValidCursor(t *testing.T) {
	cfg := DefaultConfig()
	cursor := EncodeCursor(Cursor{ID: 5, CreatedAt: fixedTime})

	var receivedParams ListParams
	mock := &internalMock{
		ListFn: func(_ context.Context, p ListParams) ([]Note, error) {
			receivedParams = p
			return []Note{{ID: 6}}, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	_, err := svc.ListNotes(context.Background(), cursor, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedParams.After == nil {
		t.Fatal("expected After cursor to be passed to store")
	}
	if receivedParams.After.ID != 5 {
		t.Errorf("After.ID = %d, want 5", receivedParams.After.ID)
	}
}

// ── GetNote ──────────────────────────────────────────────────────────────────

func TestGetNote_Found(t *testing.T) {
	want := Note{ID: 1, Content: "hello", Title: "hello", WordCount: 1, CreatedAt: fixedTime, UpdatedAt: fixedTime}
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, id int) (Note, error) {
			if id != 1 {
				t.Errorf("GetByID called with id=%d, want 1", id)
			}
			return want, nil
		},
	}
	svc := NewNotesService(mock, DefaultConfig())

	got, err := svc.GetNote(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID || got.Content != want.Content {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetNote_NotFound(t *testing.T) {
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, _ int) (Note, error) {
			return Note{}, ErrNoteNotFound
		},
	}
	svc := NewNotesService(mock, DefaultConfig())

	_, err := svc.GetNote(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for missing note")
	}
	if !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("expected ErrNoteNotFound, got %v", err)
	}
}

// ── CreateNote ───────────────────────────────────────────────────────────────

func TestCreateNote_HappyPath(t *testing.T) {
	cfg := Config{MaxContentLength: 100, MaxTitleLength: 20}
	var received Note
	mock := &internalMock{
		CreateFn: func(_ context.Context, n Note) (Note, error) {
			received = n
			n.ID = 42
			return n, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	got, err := svc.CreateNote(context.Background(), "  Hello World  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify trimming
	if received.Content != "Hello World" {
		t.Errorf("content not trimmed: got %q", received.Content)
	}

	// Verify derived title
	if received.Title != "Hello World" {
		t.Errorf("title = %q, want %q", received.Title, "Hello World")
	}

	// Verify word count
	if received.WordCount != 2 {
		t.Errorf("WordCount = %d, want 2", received.WordCount)
	}

	// Verify timestamps are recent
	if time.Since(received.CreatedAt) > 2*time.Second {
		t.Error("CreatedAt is not recent")
	}
	if !received.CreatedAt.Equal(received.UpdatedAt) {
		t.Error("CreatedAt and UpdatedAt should be equal on create")
	}

	// Verify returned ID from store
	if got.ID != 42 {
		t.Errorf("ID = %d, want 42", got.ID)
	}
}

func TestCreateNote_EmptyContent(t *testing.T) {
	svc := NewNotesService(nil, DefaultConfig())

	_, err := svc.CreateNote(context.Background(), "")
	if !errors.Is(err, ErrInvalidContent) {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestCreateNote_WhitespaceOnly(t *testing.T) {
	svc := NewNotesService(nil, DefaultConfig())

	_, err := svc.CreateNote(context.Background(), "   \t\n   ")
	if !errors.Is(err, ErrInvalidContent) {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestCreateNote_TooLong(t *testing.T) {
	cfg := Config{MaxContentLength: 10, MaxTitleLength: 50}
	svc := NewNotesService(nil, cfg)

	_, err := svc.CreateNote(context.Background(), "12345678901") // 11 chars
	if !errors.Is(err, ErrContentTooLong) {
		t.Errorf("expected ErrContentTooLong, got %v", err)
	}
}

func TestCreateNote_ExactlyMaxLength(t *testing.T) {
	cfg := Config{MaxContentLength: 10, MaxTitleLength: 50}
	mock := &internalMock{
		CreateFn: func(_ context.Context, n Note) (Note, error) {
			n.ID = 1
			return n, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	_, err := svc.CreateNote(context.Background(), "1234567890") // exactly 10
	if err != nil {
		t.Fatalf("exactly-at-limit should succeed, got: %v", err)
	}
}

func TestCreateNote_StoreError(t *testing.T) {
	cfg := Config{MaxContentLength: 100, MaxTitleLength: 50}
	mock := &internalMock{
		CreateFn: func(_ context.Context, _ Note) (Note, error) {
			return Note{}, fmt.Errorf("disk full")
		},
	}
	svc := NewNotesService(mock, cfg)

	_, err := svc.CreateNote(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when store.Create fails")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error should wrap store error, got: %v", err)
	}
}

// ── UpdateNote ───────────────────────────────────────────────────────────────

func TestUpdateNote_HappyPath(t *testing.T) {
	cfg := Config{MaxContentLength: 100, MaxTitleLength: 20}
	originalCreated := fixedTime

	mock := &internalMock{
		GetByIDFn: func(_ context.Context, id int) (Note, error) {
			return Note{
				ID:        id,
				Content:   "old content",
				Title:     "old content",
				WordCount: 2,
				CreatedAt: originalCreated,
				UpdatedAt: originalCreated,
			}, nil
		},
		UpdateFn: func(_ context.Context, n Note) (Note, error) {
			return n, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	got, err := svc.UpdateNote(context.Background(), 1, "  new content  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Content should be trimmed
	if got.Content != "new content" {
		t.Errorf("Content = %q, want %q", got.Content, "new content")
	}

	// Title should be recomputed
	if got.Title != "new content" {
		t.Errorf("Title = %q, want %q", got.Title, "new content")
	}

	// WordCount should be recomputed
	if got.WordCount != 2 {
		t.Errorf("WordCount = %d, want 2", got.WordCount)
	}

	// CreatedAt should be preserved
	if !got.CreatedAt.Equal(originalCreated) {
		t.Errorf("CreatedAt changed: got %v, want %v", got.CreatedAt, originalCreated)
	}

	// UpdatedAt should be refreshed
	if time.Since(got.UpdatedAt) > 2*time.Second {
		t.Error("UpdatedAt is not recent")
	}
}

func TestUpdateNote_NotFound(t *testing.T) {
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, _ int) (Note, error) {
			return Note{}, ErrNoteNotFound
		},
	}
	svc := NewNotesService(mock, DefaultConfig())

	_, err := svc.UpdateNote(context.Background(), 999, "new content")
	if !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("expected ErrNoteNotFound, got %v", err)
	}
}

func TestUpdateNote_EmptyContent(t *testing.T) {
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, _ int) (Note, error) {
			return Note{ID: 1, Content: "old"}, nil
		},
	}
	svc := NewNotesService(mock, DefaultConfig())

	_, err := svc.UpdateNote(context.Background(), 1, "   ")
	if !errors.Is(err, ErrInvalidContent) {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestUpdateNote_TooLong(t *testing.T) {
	cfg := Config{MaxContentLength: 5, MaxTitleLength: 50}
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, _ int) (Note, error) {
			return Note{ID: 1, Content: "old"}, nil
		},
	}
	svc := NewNotesService(mock, cfg)

	_, err := svc.UpdateNote(context.Background(), 1, "123456")
	if !errors.Is(err, ErrContentTooLong) {
		t.Errorf("expected ErrContentTooLong, got %v", err)
	}
}

func TestUpdateNote_StoreError(t *testing.T) {
	cfg := Config{MaxContentLength: 100, MaxTitleLength: 50}
	mock := &internalMock{
		GetByIDFn: func(_ context.Context, _ int) (Note, error) {
			return Note{ID: 1, Content: "old"}, nil
		},
		UpdateFn: func(_ context.Context, _ Note) (Note, error) {
			return Note{}, fmt.Errorf("update failed")
		},
	}
	svc := NewNotesService(mock, cfg)

	_, err := svc.UpdateNote(context.Background(), 1, "new content")
	if err == nil {
		t.Fatal("expected error when store.Update fails")
	}
	if !strings.Contains(err.Error(), "update failed") {
		t.Errorf("error should wrap store error, got: %v", err)
	}
}
