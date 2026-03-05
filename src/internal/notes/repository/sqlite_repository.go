// Package repository implements data access (reading/writing to the database).
//
// This is the "Repository" pattern: it hides all database-specific code behind
// a clean interface. The service layer calls repository methods without knowing
// whether data comes from SQLite, PostgreSQL, an in-memory map, or even a
// remote API.
//
// WHAT THIS LAYER DOES vs. THE SERVICE LAYER:
//   - Repository: "I know HOW to store and retrieve data" (SQL, GORM calls)
//   - Service: "I know WHAT the business rules are" (validation, derived fields)
//
// Notice that the repository does ZERO validation or computation. It doesn't
// check if content is empty, it doesn't compute titles, it doesn't count words.
// It just maps between dbschema.NoteRecord (database struct) and service.Note (domain
// struct), and runs GORM queries. That's it.
package repository

import (
	"context"
	"errors"
	"fmt"

	"notes-api/internal/notes/service"
	dbschema "notes-api/internal/platform/db/schema"

	"gorm.io/gorm"
)

// SQLiteNotesRepository is the concrete implementation of service.NotesStore.
type SQLiteNotesRepository struct {
	db *gorm.DB
}

// NewSQLiteNotesRepository creates a new SQLiteNotesRepository with the given DB handle.
func NewSQLiteNotesRepository(db *gorm.DB) *SQLiteNotesRepository {
	return &SQLiteNotesRepository{db: db}
}

// List retrieves a page of notes using cursor-based pagination with sort support.
// The sort order determines the ORDER BY clause. When a cursor is provided,
// a composite WHERE clause like (created_at, id) > (?, ?) ensures stable
// pagination even when the sort column has duplicate values (the ID tiebreaker
// guarantees uniqueness).
func (r *SQLiteNotesRepository) List(ctx context.Context, params service.ListParams) ([]service.Note, error) {
	var records []dbschema.NoteRecord

	query := r.db.WithContext(ctx)

	// Apply cursor filter + ORDER BY based on sort order.
	switch params.Sort {
	case service.SortIDDesc:
		if params.After != nil {
			query = query.Where("id < ?", params.After.ID)
		}
		query = query.Order("id DESC")

	case service.SortCreatedAtAsc:
		if params.After != nil {
			// Composite cursor: (created_at, id) > (cursorCreatedAt, cursorID)
			query = query.Where(
				"(created_at > ?) OR (created_at = ? AND id > ?)",
				params.After.CreatedAt, params.After.CreatedAt, params.After.ID,
			)
		}
		query = query.Order("created_at ASC, id ASC")

	case service.SortCreatedAtDesc:
		if params.After != nil {
			// Composite cursor: (created_at, id) < (cursorCreatedAt, cursorID)
			query = query.Where(
				"(created_at < ?) OR (created_at = ? AND id < ?)",
				params.After.CreatedAt, params.After.CreatedAt, params.After.ID,
			)
		}
		query = query.Order("created_at DESC, id DESC")

	default: // SortIDAsc (default)
		if params.After != nil {
			query = query.Where("id > ?", params.After.ID)
		}
		query = query.Order("id ASC")
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}

	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query notes: %w", err)
	}

	notes := make([]service.Note, 0, len(records))
	for _, record := range records {
		notes = append(notes, toServiceNote(record))
	}
	return notes, nil
}

// GetByID retrieves a single note by its primary key.
// If the note doesn't exist, it returns service.ErrNoteNotFound.
//
// KEY POINT: The repository translates GORM's gorm.ErrRecordNotFound into the
// service layer's ErrNoteNotFound. This way, the service layer doesn't need to
// know about GORM error types — it only deals with its own domain errors.
func (r *SQLiteNotesRepository) GetByID(ctx context.Context, id int) (service.Note, error) {
	var record dbschema.NoteRecord
	if err := r.db.WithContext(ctx).First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Translate GORM error → domain error
			return service.Note{}, service.ErrNoteNotFound
		}
		return service.Note{}, fmt.Errorf("query note %d: %w", id, err)
	}
	return toServiceNote(record), nil
}

// Create inserts a new note into the database.
// The service has already computed Title, WordCount, and timestamps.
// The repository just stores whatever it receives — no business logic here.
func (r *SQLiteNotesRepository) Create(ctx context.Context, note service.Note) (service.Note, error) {
	record := toModelNote(note)
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		return service.Note{}, fmt.Errorf("insert note: %w", err)
	}
	// After Create(), record.ID is populated with the auto-generated value
	return toServiceNote(record), nil
}

// Update saves changes to an existing note in the database.
// It performs a strict UPDATE by primary key and returns ErrNoteNotFound when
// no row matches, avoiding GORM Save()'s upsert behavior.
func (r *SQLiteNotesRepository) Update(ctx context.Context, note service.Note) (service.Note, error) {
	result := r.db.WithContext(ctx).
		Model(&dbschema.NoteRecord{}).
		Where("id = ?", note.ID).
		Updates(map[string]interface{}{
			"content":    note.Content,
			"title":      note.Title,
			"word_count": note.WordCount,
			"updated_at": note.UpdatedAt,
		})
	if result.Error != nil {
		return service.Note{}, fmt.Errorf("update note %d: %w", note.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return service.Note{}, service.ErrNoteNotFound
	}
	return note, nil
}

// ── Mapping functions ────────────────────────────────────────────────────────
// These convert between dbschema.NoteRecord (database) and service.Note (domain).
//
// WHY NOT USE THE SAME STRUCT FOR BOTH?
// Because they serve different purposes:
//   - dbschema.NoteRecord has GORM tags (primaryKey, not null, type:text) — database concerns
//   - service.Note has no tags — it's a pure domain object
//
// If we used one struct for both, changing a GORM tag could accidentally break
// the service layer, or the service would depend on the GORM package (bad!).

func toServiceNote(m dbschema.NoteRecord) service.Note {
	return service.Note{
		ID:        m.ID,
		Content:   m.Content,
		Title:     m.Title,
		WordCount: m.WordCount,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func toModelNote(s service.Note) dbschema.NoteRecord {
	return dbschema.NoteRecord{
		ID:        s.ID,
		Content:   s.Content,
		Title:     s.Title,
		WordCount: s.WordCount,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
