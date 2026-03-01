package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"notes-api/internal/db"
	"notes-api/internal/model"
	"notes-api/internal/service"
)

// openTestDB creates a fresh in-memory SQLite database with migrated schema.
func openTestDB(t *testing.T) *NotesRepository {
	t.Helper()
	ctx := context.Background()
	gormDB, err := db.OpenSQLite(ctx, ":memory:", db.DefaultConfig())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Migrate(ctx, gormDB); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gormDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return NewNotesRepository(gormDB)
}

var fixedTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func makeNote(id int) service.Note {
	return service.Note{
		Content:   "content " + string(rune('A'+id-1)),
		Title:     "title",
		WordCount: 2,
		CreatedAt: fixedTime.Add(time.Duration(id) * time.Second),
		UpdatedAt: fixedTime.Add(time.Duration(id) * time.Second),
	}
}

// ── Create ───────────────────────────────────────────────────────────────────

func TestCreate(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	note := makeNote(1)
	created, err := repo.Create(ctx, note)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Error("expected auto-generated ID > 0")
	}
	if created.Content != note.Content {
		t.Errorf("Content = %q, want %q", created.Content, note.Content)
	}
}

// ── GetByID ──────────────────────────────────────────────────────────────────

func TestGetByID_Found(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	created, _ := repo.Create(ctx, makeNote(1))

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.Content != created.Content {
		t.Errorf("Content = %q, want %q", got.Content, created.Content)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	if err == nil {
		t.Fatal("expected error for non-existent ID")
	}
	if err != service.ErrNoteNotFound {
		t.Errorf("expected ErrNoteNotFound, got %v", err)
	}
}

// ── Update ───────────────────────────────────────────────────────────────────

func TestUpdate(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	created, _ := repo.Create(ctx, makeNote(1))

	created.Content = "updated content"
	created.Title = "updated"
	updated, err := repo.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Content != "updated content" {
		t.Errorf("Content = %q, want %q", updated.Content, "updated content")
	}

	// Verify persistence
	got, _ := repo.GetByID(ctx, created.ID)
	if got.Content != "updated content" {
		t.Errorf("persisted Content = %q, want %q", got.Content, "updated content")
	}
}

func TestUpdate_NotFound_DoesNotInsert(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	missing := makeNote(1)
	missing.ID = 999

	_, err := repo.Update(ctx, missing)
	if !errors.Is(err, service.ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound, got %v", err)
	}

	_, err = repo.GetByID(ctx, missing.ID)
	if !errors.Is(err, service.ErrNoteNotFound) {
		t.Fatalf("expected note to remain missing, got %v", err)
	}
}

// ── List – sort orders ───────────────────────────────────────────────────────

func seedNotes(t *testing.T, repo *NotesRepository, n int) []service.Note {
	t.Helper()
	ctx := context.Background()
	result := make([]service.Note, 0, n)
	for i := 1; i <= n; i++ {
		created, err := repo.Create(ctx, makeNote(i))
		if err != nil {
			t.Fatalf("seed note %d: %v", i, err)
		}
		result = append(result, created)
	}
	return result
}

func TestList_SortIDAsc(t *testing.T) {
	repo := openTestDB(t)
	notes := seedNotes(t, repo, 3)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortIDAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != notes[0].ID || got[2].ID != notes[2].ID {
		t.Errorf("expected ascending ID order: %d, %d, %d", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestList_SortIDDesc(t *testing.T) {
	repo := openTestDB(t)
	notes := seedNotes(t, repo, 3)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortIDDesc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].ID != notes[2].ID || got[2].ID != notes[0].ID {
		t.Errorf("expected descending ID order: %d, %d, %d", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestList_SortCreatedAtAsc(t *testing.T) {
	repo := openTestDB(t)
	seedNotes(t, repo, 3)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortCreatedAtAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for i := 1; i < len(got); i++ {
		if got[i].CreatedAt.Before(got[i-1].CreatedAt) {
			t.Errorf("expected ascending CreatedAt at index %d", i)
		}
	}
}

func TestList_SortCreatedAtDesc(t *testing.T) {
	repo := openTestDB(t)
	seedNotes(t, repo, 3)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortCreatedAtDesc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for i := 1; i < len(got); i++ {
		if got[i].CreatedAt.After(got[i-1].CreatedAt) {
			t.Errorf("expected descending CreatedAt at index %d", i)
		}
	}
}

// ── List – pagination ────────────────────────────────────────────────────────

func TestList_Limit(t *testing.T) {
	repo := openTestDB(t)
	seedNotes(t, repo, 5)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortIDAsc,
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestList_CursorIDAsc(t *testing.T) {
	repo := openTestDB(t)
	notes := seedNotes(t, repo, 5)

	// Get notes after ID 2
	cursor := &service.Cursor{ID: notes[1].ID, CreatedAt: notes[1].CreatedAt}
	got, err := repo.List(context.Background(), service.ListParams{
		After: cursor,
		Sort:  service.SortIDAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (notes 3,4,5)", len(got))
	}
	if got[0].ID != notes[2].ID {
		t.Errorf("first note ID = %d, want %d", got[0].ID, notes[2].ID)
	}
}

func TestList_CursorIDDesc(t *testing.T) {
	repo := openTestDB(t)
	notes := seedNotes(t, repo, 5)

	// Get notes with ID less than note 4 (descending)
	cursor := &service.Cursor{ID: notes[3].ID, CreatedAt: notes[3].CreatedAt}
	got, err := repo.List(context.Background(), service.ListParams{
		After: cursor,
		Sort:  service.SortIDDesc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (notes 3,2,1)", len(got))
	}
	// Should be descending
	if got[0].ID >= notes[3].ID {
		t.Errorf("first note ID = %d, should be < %d", got[0].ID, notes[3].ID)
	}
}

func TestList_EmptyDB(t *testing.T) {
	repo := openTestDB(t)

	got, err := repo.List(context.Background(), service.ListParams{
		Sort:  service.SortIDAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ── Mapping functions ────────────────────────────────────────────────────────

func TestToServiceNote_RoundTrip(t *testing.T) {
	m := model.Note{
		ID:        1,
		Content:   "hello",
		Title:     "hello",
		WordCount: 1,
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}

	s := toServiceNote(m)
	if s.ID != m.ID || s.Content != m.Content || s.Title != m.Title ||
		s.WordCount != m.WordCount || !s.CreatedAt.Equal(m.CreatedAt) || !s.UpdatedAt.Equal(m.UpdatedAt) {
		t.Errorf("toServiceNote mismatch: got %+v from %+v", s, m)
	}

	back := toModelNote(s)
	if back.ID != m.ID || back.Content != m.Content || back.Title != m.Title ||
		back.WordCount != m.WordCount || !back.CreatedAt.Equal(m.CreatedAt) || !back.UpdatedAt.Equal(m.UpdatedAt) {
		t.Errorf("toModelNote mismatch: got %+v from %+v", back, s)
	}
}
