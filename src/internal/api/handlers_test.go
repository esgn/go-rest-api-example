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

	"notes-api/internal/api/middleware"
	gen "notes-api/internal/gen"
	"notes-api/internal/service"
	"notes-api/internal/testutil"
)

var fixedTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func newTestHTTPHandler(mock *testutil.MockNotesStore, maxBodyBytes int64) http.Handler {
	cfg := service.Config{
		MaxContentLength: 100,
		MaxTitleLength:   20,
		DefaultPageLimit: 20,
		MaxPageLimit:     100,
	}
	svc := service.NewNotesService(mock, cfg)
	strictImpl := NewNotesHandler(svc)

	strictServer := gen.NewStrictHandlerWithOptions(strictImpl, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		},
	})

	return gen.HandlerWithOptions(strictServer, gen.StdHTTPServerOptions{
		Middlewares: []gen.MiddlewareFunc{
			middleware.RejectUnknownJSONFields(),
			middleware.EnforceQueryRules(512, cfg.MaxPageLimit),
			middleware.RejectUnknownQueryParams(),
			middleware.EnforceBodyAndContentType(maxBodyBytes),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		},
	})
}

func newDefaultTestHTTPHandler(mock *testutil.MockNotesStore) http.Handler {
	return newTestHTTPHandler(mock, 1024)
}

func doRequest(t *testing.T, h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder, v interface{}) {
	t.Helper()

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal body %q: %v", string(data), err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

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

func TestCreateNoteHandler_Valid(t *testing.T) {
	mock := &testutil.MockNotesStore{
		CreateFn: func(_ context.Context, n service.Note) (service.Note, error) {
			n.ID = 1
			return n, nil
		},
	}
	h := newDefaultTestHTTPHandler(mock)

	rr := doRequest(t, h, http.MethodPost, "/notes", "application/json", `{"content":"hello world"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusCreated)
	}

	var note gen.Note
	decodeBody(t, rr, &note)
	if note.Id != 1 {
		t.Errorf("Id = %d, want 1", note.Id)
	}
	if note.Content != "hello world" {
		t.Errorf("Content = %q, want %q", note.Content, "hello world")
	}
}

func TestCreateNoteHandler_BadInputs(t *testing.T) {
	h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})

	tests := []struct {
		name        string
		target      string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "empty body", target: "/notes", contentType: "application/json", body: "", wantStatus: http.StatusBadRequest},
		{name: "malformed json", target: "/notes", contentType: "application/json", body: `{invalid`, wantStatus: http.StatusBadRequest},
		{name: "unknown fields", target: "/notes", contentType: "application/json", body: `{"content":"hi","extra":123}`, wantStatus: http.StatusBadRequest},
		{name: "unknown query", target: "/notes?foo=bar", contentType: "application/json", body: `{"content":"hello"}`, wantStatus: http.StatusBadRequest},
		{name: "unsupported media type", target: "/notes", contentType: "text/plain", body: `{"content":"hello"}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "empty content", target: "/notes", contentType: "application/json", body: `{"content":""}`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := doRequest(t, h, http.MethodPost, tt.target, tt.contentType, tt.body)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestCreateNoteHandler_OversizedBody(t *testing.T) {
	h := newTestHTTPHandler(&testutil.MockNotesStore{}, 32)
	body := `{"content":"` + strings.Repeat("a", 1000) + `"}`

	rr := doRequest(t, h, http.MethodPost, "/notes", "application/json", body)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestGetNoteHandler(t *testing.T) {
	t.Run("found", func(t *testing.T) {
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
		h := newDefaultTestHTTPHandler(mock)

		rr := doRequest(t, h, http.MethodGet, "/notes/1", "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var note gen.Note
		decodeBody(t, rr, &note)
		if note.Id != 1 {
			t.Errorf("Id = %d, want 1", note.Id)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock := &testutil.MockNotesStore{
			GetByIDFn: func(_ context.Context, _ int) (service.Note, error) {
				return service.Note{}, service.ErrNoteNotFound
			},
		}
		h := newDefaultTestHTTPHandler(mock)

		rr := doRequest(t, h, http.MethodGet, "/notes/999", "", "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("unknown query", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes/1?foo=bar", "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})
}

func TestUpdateNoteHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		mock := &testutil.MockNotesStore{
			GetByIDFn: func(_ context.Context, id int) (service.Note, error) {
				return service.Note{ID: id, Content: "old", CreatedAt: fixedTime, UpdatedAt: fixedTime}, nil
			},
			UpdateFn: func(_ context.Context, n service.Note) (service.Note, error) {
				return n, nil
			},
		}
		h := newDefaultTestHTTPHandler(mock)

		rr := doRequest(t, h, http.MethodPut, "/notes/1", "application/json", `{"content":"updated"}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var note gen.Note
		decodeBody(t, rr, &note)
		if note.Content != "updated" {
			t.Errorf("Content = %q, want %q", note.Content, "updated")
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock := &testutil.MockNotesStore{
			GetByIDFn: func(_ context.Context, _ int) (service.Note, error) {
				return service.Note{}, service.ErrNoteNotFound
			},
		}
		h := newDefaultTestHTTPHandler(mock)

		rr := doRequest(t, h, http.MethodPut, "/notes/999", "application/json", `{"content":"updated"}`)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodPut, "/notes/1", "application/json", `not json`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("oversized body", func(t *testing.T) {
		h := newTestHTTPHandler(&testutil.MockNotesStore{}, 32)
		body := `{"content":"` + strings.Repeat("x", 1000) + `"}`
		rr := doRequest(t, h, http.MethodPut, "/notes/1", "application/json", body)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
		}
	})

	t.Run("unknown query", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodPut, "/notes/1?foo=bar", "application/json", `{"content":"updated"}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("unsupported media type", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodPut, "/notes/1", "text/plain", `{"content":"updated"}`)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnsupportedMediaType)
		}
	})
}

func TestListNotesHandler(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		mock := &testutil.MockNotesStore{
			ListFn: func(_ context.Context, _ service.ListParams) ([]service.Note, error) {
				return []service.Note{{ID: 1, Content: "one", Title: "one", WordCount: 1, CreatedAt: fixedTime, UpdatedAt: fixedTime}}, nil
			},
		}
		h := newDefaultTestHTTPHandler(mock)

		rr := doRequest(t, h, http.MethodGet, "/notes", "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
		}

		var list gen.NoteList
		decodeBody(t, rr, &list)
		if len(list.Data) != 1 {
			t.Fatalf("len(Data) = %d, want 1", len(list.Data))
		}
		if list.HasMore {
			t.Error("HasMore should be false")
		}
	})

	t.Run("unknown query", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes?foo=bar", "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("explicit zero limit", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes?limit=0", "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("explicit empty after", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes?after=", "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("too long after", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes?after="+strings.Repeat("a", 513), "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("limit above max", func(t *testing.T) {
		h := newDefaultTestHTTPHandler(&testutil.MockNotesStore{})
		rr := doRequest(t, h, http.MethodGet, "/notes?limit=101", "", "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})
}
