package service

import (
	"errors"
	"testing"
)

// ── countWords ───────────────────────────────────────────────────────────────

func TestCountWords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty string", "", 0},
		{"whitespace only", "   \t\n  ", 0},
		{"single word", "hello", 1},
		{"two words", "hello world", 2},
		{"multiple spaces", "  hello   world  ", 2},
		{"tabs and newlines", "one\ttwo\nthree", 3},
		{"unicode words", "café résumé naïve", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countWords(tt.input)
			if got != tt.want {
				t.Errorf("countWords(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ── deriveTitle ──────────────────────────────────────────────────────────────

func TestDeriveTitle(t *testing.T) {
	// Use a small MaxTitleLength for easy-to-read test cases.
	svc := NewNotesService(nil, Config{MaxTitleLength: 20})

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "short single line",
			content: "Buy milk",
			want:    "Buy milk",
		},
		{
			name:    "exactly max length",
			content: "12345678901234567890", // 20 chars
			want:    "12345678901234567890",
		},
		{
			name:    "exceeds max truncates at word boundary",
			content: "This is a fairly long title that exceeds the limit",
			want:    "This is a fairly…",
		},
		{
			name:    "multiline takes first line only",
			content: "First line\nSecond line\nThird line",
			want:    "First line",
		},
		{
			name:    "multiline long first line",
			content: "This is a fairly long title that exceeds\nSecond line",
			want:    "This is a fairly…",
		},
		{
			name:    "single long word truncated without space",
			content: "abcdefghijklmnopqrstuvwxyz", // no spaces → truncates at max
			want:    "abcdefghijklmnopqrst…",
		},
		{
			name:    "leading/trailing whitespace on first line",
			content: "  hello world  \nsecond",
			want:    "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.deriveTitle(tt.content)
			if got != tt.want {
				t.Errorf("deriveTitle(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

// ── ParseSortOrder ───────────────────────────────────────────────────────────

func TestParseSortOrder(t *testing.T) {
	tests := []struct {
		input   string
		want    SortOrder
		wantErr bool
	}{
		{"", SortIDAsc, false},
		{"id", SortIDAsc, false},
		{"-id", SortIDDesc, false},
		{"createdAt", SortCreatedAtAsc, false},
		{"-createdAt", SortCreatedAtDesc, false},
		{"invalid", "", true},
		{"updatedAt", "", true},
	}

	for _, tt := range tests {
		t.Run("sort="+tt.input, func(t *testing.T) {
			got, err := ParseSortOrder(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSortOrder(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseSortOrder(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if tt.wantErr {
				if !isErr(err, ErrInvalidSort) {
					t.Errorf("expected ErrInvalidSort, got %v", err)
				}
			}
		})
	}
}

// ── EncodeCursor / DecodeCursor ──────────────────────────────────────────────

func TestCursorRoundTrip(t *testing.T) {
	original := Cursor{ID: 42, CreatedAt: fixedTime}

	encoded := EncodeCursor(original)
	if encoded == "" {
		t.Fatal("EncodeCursor returned empty string")
	}

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor(%q) error: %v", encoded, err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %d, want %d", decoded.ID, original.ID)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", decoded.CreatedAt, original.CreatedAt)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not base64", "!!!invalid!!!"},
		{"valid base64 but not JSON", "aGVsbG8"}, // "hello"
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCursor(tt.input)
			if err == nil {
				t.Error("expected error, got nil")
			}
			if !isErr(err, ErrInvalidCursor) {
				t.Errorf("expected ErrInvalidCursor, got %v", err)
			}
		})
	}
}

// ── DefaultConfig ────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxContentLength != defaultMaxContentLength {
		t.Errorf("MaxContentLength = %d, want %d", cfg.MaxContentLength, defaultMaxContentLength)
	}
	if cfg.MaxTitleLength != defaultMaxTitleLength {
		t.Errorf("MaxTitleLength = %d, want %d", cfg.MaxTitleLength, defaultMaxTitleLength)
	}
	if cfg.DefaultPageLimit != defaultPageLimit {
		t.Errorf("DefaultPageLimit = %d, want %d", cfg.DefaultPageLimit, defaultPageLimit)
	}
	if cfg.MaxPageLimit != defaultMaxPageLimit {
		t.Errorf("MaxPageLimit = %d, want %d", cfg.MaxPageLimit, defaultMaxPageLimit)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func isErr(err, target error) bool {
	return err != nil && errors.Is(err, target)
}
