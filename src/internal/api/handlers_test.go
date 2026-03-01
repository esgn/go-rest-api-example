package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gen "notes-api/internal/gen"
	"notes-api/internal/service"
	"notes-api/internal/testutil"
)

// ── helpers ──────────────────────────────────────────────────────────────────

var fixedTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

// newTestHandler builds a NotesHandler backed by a real NotesService whose
// store is the supplied mock. maxBodyBytes defaults to a generous limit.
func newTestHandler(mock *testutil.MockNotesStore) *NotesHandler {
	cfg := service.Config{
		MaxContentLength: 100,
		MaxTitleLength:   20,
		DefaultPageLimit: 20,
		MaxPageLimit:     100,
	}
	svc := service.NewNotesService(mock, cfg)
	return NewNotesHandler(svc, 1024) // 1 KB body limit for tests
}

// decodeBody reads a JSON response body into the given pointer.
func decodeBody(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal body %q: %v", string(data), err)
	}
}

// ── toAPINote ────────────────────────────────────────────────────────────────

func TestToAPINote(t *testing.T) {
	n := service.Note{
		ID:        1,
		Content:   "hello",
		Title:     "hello",
		WordCount: 1,
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}
	got := toAPINote(n)

	if got.Id != 1 {
		t.Errorf("Id = %d, want 1", got.Id)
	}
	if got.Content != "hello" {
		t.Errorf("Content = %q, want %q", got.Content, "hello")
	}
	if got.Title != "hello" {
		t.Errorf("Title = %q, want %q", got.Title, "hello")
	}
	if got.WordCount != 1 {
		t.Errorf("WordCount = %d, want 1", got.WordCount)
	}
	if !got.CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, fixedTime)
	}
	if !got.UpdatedAt.Equal(fixedTime) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, fixedTime)
	}
}

// ── writeJSON ────────────────────────────────────────────────────────────────

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"key": "value"})

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	var body map[string]string
	decodeBody(t, resp, &body)
	if body["key"] != "value" {
		t.Errorf("body[\"key\"] = %q, want %q", body["key"], "value")
	}
}

// ── writeError ───────────────────────────────────────────────────────────────

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "oops")

	resp := rec.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var body map[string]string
	decodeBody(t, resp, &body)
	if body["error"] != "oops" {
		t.Errorf("body[\"error\"] = %q, want %q", body["error"], "oops")
	}
}

func TestAllowedQueryParamsFromStruct_ListNotes(t *testing.T) {
	allowed := allowedQueryParamsFromStruct[gen.ListNotesParams]()

	for _, key := range []string{"after", "limit", "sort"} {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("missing expected query param key %q", key)
		}
	}
	if len(allowed) != 3 {
		t.Fatalf("allowed key count = %d, want 3", len(allowed))
	}
}

// ── writeServiceError ────────────────────────────────────────────────────────

func TestWriteServiceError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"ErrInvalidContent", service.ErrInvalidContent, http.StatusBadRequest},
		{"ErrContentTooLong", service.ErrContentTooLong, http.StatusBadRequest},
		{"ErrInvalidCursor", service.ErrInvalidCursor, http.StatusBadRequest},
		{"ErrInvalidSort", service.ErrInvalidSort, http.StatusBadRequest},
		{"ErrInvalidLimit", service.ErrInvalidLimit, http.StatusBadRequest},
		{"ErrNoteNotFound", service.ErrNoteNotFound, http.StatusNotFound},
		{"unknown error", io.ErrUnexpectedEOF, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeServiceError(rec, tt.err)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// ── CreateNote handler ───────────────────────────────────────────────────────

func TestCreateNote_Handler_Valid(t *testing.T) {
	mock := &testutil.MockNotesStore{
		CreateFn: func(_ context.Context, n service.Note) (service.Note, error) {
			n.ID = 1
			return n, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(`{"content":"hello world"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var note gen.Note
	decodeBody(t, rec.Result(), &note)
	if note.Id != 1 {
		t.Errorf("Id = %d, want 1", note.Id)
	}
	if note.Content != "hello world" {
		t.Errorf("Content = %q, want %q", note.Content, "hello world")
	}
}

func TestCreateNote_Handler_EmptyBody(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateNote_Handler_MalformedJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateNote_Handler_UnknownFields(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(`{"content":"hi","extra":123}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateNote_Handler_OversizedBody(t *testing.T) {
	mock := &testutil.MockNotesStore{}
	// Build a handler with a tiny 32-byte body limit
	svc := service.NewNotesService(mock, service.Config{
		MaxContentLength: 10000,
		MaxTitleLength:   50,
		DefaultPageLimit: 20,
		MaxPageLimit:     100,
	})
	h := NewNotesHandler(svc, 32)

	body := `{"content":"` + strings.Repeat("a", 1000) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCreateNote_Handler_EmptyContent(t *testing.T) {
	mock := &testutil.MockNotesStore{}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(`{"content":""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	// Empty content → service returns ErrInvalidContent → handler returns 400
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateNote_Handler_RejectsUnknownQueryParam(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPost, "/notes?foo=bar", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateNote(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ── GetNote handler ──────────────────────────────────────────────────────────

func TestGetNote_Handler_Found(t *testing.T) {
	mock := &testutil.MockNotesStore{
		GetByIDFn: func(_ context.Context, id int) (service.Note, error) {
			return service.Note{
				ID:        id,
				Content:   "hello",
				Title:     "hello",
				WordCount: 1,
				CreatedAt: fixedTime,
				UpdatedAt: fixedTime,
			}, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/notes/1", nil)
	rec := httptest.NewRecorder()

	h.GetNote(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var note gen.Note
	decodeBody(t, rec.Result(), &note)
	if note.Id != 1 {
		t.Errorf("Id = %d, want 1", note.Id)
	}
}

func TestGetNote_Handler_NotFound(t *testing.T) {
	mock := &testutil.MockNotesStore{
		GetByIDFn: func(_ context.Context, _ int) (service.Note, error) {
			return service.Note{}, service.ErrNoteNotFound
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/notes/999", nil)
	rec := httptest.NewRecorder()

	h.GetNote(rec, req, 999)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetNote_Handler_RejectsUnknownQueryParam(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodGet, "/notes/1?foo=bar", nil)
	rec := httptest.NewRecorder()

	h.GetNote(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ── UpdateNote handler ───────────────────────────────────────────────────────

func TestUpdateNote_Handler_Valid(t *testing.T) {
	mock := &testutil.MockNotesStore{
		GetByIDFn: func(_ context.Context, id int) (service.Note, error) {
			return service.Note{ID: id, Content: "old", CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
		},
		UpdateFn: func(_ context.Context, n service.Note) (service.Note, error) {
			return n, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/notes/1", strings.NewReader(`{"content":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateNote(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var note gen.Note
	decodeBody(t, rec.Result(), &note)
	if note.Content != "updated" {
		t.Errorf("Content = %q, want %q", note.Content, "updated")
	}
}

func TestUpdateNote_Handler_NotFound(t *testing.T) {
	mock := &testutil.MockNotesStore{
		GetByIDFn: func(_ context.Context, _ int) (service.Note, error) {
			return service.Note{}, service.ErrNoteNotFound
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPut, "/notes/999", strings.NewReader(`{"content":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateNote(rec, req, 999)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateNote_Handler_InvalidBody(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPut, "/notes/1", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateNote(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateNote_Handler_OversizedBody(t *testing.T) {
	svc := service.NewNotesService(&testutil.MockNotesStore{}, service.Config{
		MaxContentLength: 10000,
		MaxTitleLength:   50,
		DefaultPageLimit: 20,
		MaxPageLimit:     100,
	})
	h := NewNotesHandler(svc, 32) // tiny limit

	body := `{"content":"` + strings.Repeat("x", 1000) + `"}`
	req := httptest.NewRequest(http.MethodPut, "/notes/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateNote(rec, req, 1)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestUpdateNote_Handler_RejectsUnknownQueryParam(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodPut, "/notes/1?foo=bar", strings.NewReader(`{"content":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateNote(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ── ListNotes handler ────────────────────────────────────────────────────────

func TestListNotes_Handler_Default(t *testing.T) {
	mock := &testutil.MockNotesStore{
		ListFn: func(_ context.Context, _ service.ListParams) ([]service.Note, error) {
			return []service.Note{
				{ID: 1, Content: "one", Title: "one", WordCount: 1, CreatedAt: fixedTime, UpdatedAt: fixedTime},
			}, nil
		},
	}
	h := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	rec := httptest.NewRecorder()

	h.ListNotes(rec, req, gen.ListNotesParams{})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var list gen.NoteList
	decodeBody(t, rec.Result(), &list)
	if len(list.Data) != 1 {
		t.Errorf("len(Data) = %d, want 1", len(list.Data))
	}
	if list.HasMore {
		t.Error("HasMore should be false")
	}
}

func TestListNotes_Handler_RejectsUnknownQueryParam(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodGet, "/notes?foo=bar", nil)
	rec := httptest.NewRecorder()

	h.ListNotes(rec, req, gen.ListNotesParams{})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListNotes_Handler_RejectsExplicitZeroLimit(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodGet, "/notes?limit=0", nil)
	rec := httptest.NewRecorder()

	h.ListNotes(rec, req, gen.ListNotesParams{Limit: 0})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListNotes_Handler_RejectsExplicitEmptyAfter(t *testing.T) {
	h := newTestHandler(&testutil.MockNotesStore{})

	req := httptest.NewRequest(http.MethodGet, "/notes?after=", nil)
	rec := httptest.NewRecorder()

	h.ListNotes(rec, req, gen.ListNotesParams{After: ""})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
