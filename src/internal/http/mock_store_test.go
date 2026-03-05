package api

import (
	"context"

	"notes-api/internal/notes/service"
)

// MockNotesStore is a hand-written mock implementing service.NotesStore for HTTP tests.
type MockNotesStore struct {
	ListFn    func(ctx context.Context, params service.ListParams) ([]service.Note, error)
	GetByIDFn func(ctx context.Context, id int) (service.Note, error)
	CreateFn  func(ctx context.Context, note service.Note) (service.Note, error)
	UpdateFn  func(ctx context.Context, note service.Note) (service.Note, error)
}

var _ service.NotesStore = (*MockNotesStore)(nil)

func (m *MockNotesStore) List(ctx context.Context, params service.ListParams) ([]service.Note, error) {
	if m.ListFn == nil {
		panic("MockNotesStore.List called but ListFn not set")
	}
	return m.ListFn(ctx, params)
}

func (m *MockNotesStore) GetByID(ctx context.Context, id int) (service.Note, error) {
	if m.GetByIDFn == nil {
		panic("MockNotesStore.GetByID called but GetByIDFn not set")
	}
	return m.GetByIDFn(ctx, id)
}

func (m *MockNotesStore) Create(ctx context.Context, note service.Note) (service.Note, error) {
	if m.CreateFn == nil {
		panic("MockNotesStore.Create called but CreateFn not set")
	}
	return m.CreateFn(ctx, note)
}

func (m *MockNotesStore) Update(ctx context.Context, note service.Note) (service.Note, error) {
	if m.UpdateFn == nil {
		panic("MockNotesStore.Update called but UpdateFn not set")
	}
	return m.UpdateFn(ctx, note)
}
