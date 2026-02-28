// Package model contains GORM model structs.
//
// A "model" in GORM represents a database table. Each struct field maps to a
// column. GORM uses the struct definition to:
//   - Create/migrate the table schema (via AutoMigrate)
//   - Map query results to Go objects
//   - Build INSERT/UPDATE statements from struct values
//
// IMPORTANT: This struct is ONLY for the database layer. The API layer has its
// own gen.Note struct (with JSON tags), and the service layer has service.Note
// (a plain struct with no tags). Each layer uses its own type so they can
// evolve independently.
//
// WHY THIS MATTERS FOR LAYERED ARCHITECTURE:
// The database might store a "word_count" column for fast queries, but only the
// service layer knows HOW to compute it. The model just stores the result.
// Similarly, timestamps here use time.Time, but the API layer might format them
// differently. Each layer owns its own concerns.
package model

import "time"

// Note represents a single note stored in the database.
//
// Each field has GORM tags that control database behavior:
//   - ID:        auto-incremented primary key
//   - Content:   the raw text (stored as TEXT for unlimited length)
//   - Title:     a short summary derived by the service layer
//   - WordCount: precomputed by the service layer, stored for efficient queries
//   - CreatedAt: set once when the note is first created
//   - UpdatedAt: updated every time the note is modified
//
// Notice: Title and WordCount are "derived" fields — the service layer computes
// them from Content. The model doesn't know how they're calculated, it just
// stores whatever the service provides. This is a key benefit of the layered
// architecture: business logic lives in the service, not in the model.
type Note struct {
	ID        int       `gorm:"primaryKey;autoIncrement"`
	Content   string    `gorm:"type:text;not null;default:''"`
	Title     string    `gorm:"type:varchar(60);not null;default:''"`
	WordCount int       `gorm:"not null;default:0"`
	CreatedAt time.Time `gorm:"not null;default:'1970-01-01 00:00:00'"`
	UpdatedAt time.Time `gorm:"not null;default:'1970-01-01 00:00:00'"`
}
