package schema

import "time"

// NoteRecord is the GORM persistence model for the notes table.
type NoteRecord struct {
	ID        int       `gorm:"primaryKey;autoIncrement"`
	Content   string    `gorm:"type:text;not null;default:''"`
	Title     string    `gorm:"type:varchar(60);not null;default:''"`
	WordCount int       `gorm:"not null;default:0"`
	CreatedAt time.Time `gorm:"not null;default:'1970-01-01 00:00:00'"`
	UpdatedAt time.Time `gorm:"not null;default:'1970-01-01 00:00:00'"`
}
