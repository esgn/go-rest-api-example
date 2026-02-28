// Package api contains HTTP handlers — the "adapter" between HTTP and the
// service layer.
//
// WHAT THIS LAYER DOES vs. THE SERVICE LAYER:
//   - Handler: "I know about HTTP" (status codes, JSON parsing, path/query params)
//   - Service: "I know about business rules" (validation, derived fields, pagination)
//
// NOTE: The generated wrapper (gen/server.gen.go) handles all parameter parsing
// (path params like {id}, query params like ?after= and ?limit=) before calling
// handler methods. Handlers receive already-parsed, typed values.
//
// The handler NEVER validates content, computes titles, or normalises pagination.
// It ONLY translates between HTTP and service calls. This separation means:
//   - You can change the API format (e.g., XML instead of JSON) without
//     touching business logic.
//   - You can reuse the service from a CLI or gRPC server.
//   - Each layer is independently testable.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	gen "notes-api/internal/gen"
	"notes-api/internal/service"
)

// Compile-time check: ensure NotesHandler implements all methods in the
// generated ServerInterface (ListNotes, CreateNote, GetNote, UpdateNote).
var _ gen.ServerInterface = (*NotesHandler)(nil)

// NotesHandler implements gen.ServerInterface, handling all HTTP endpoints.
// It depends on service.NotesService (via dependency injection) to avoid
// directly depending on the database or business logic.
// The handler layer is purely about HTTP translation — parsing requests,
// calling the service, mapping responses to JSON.
type NotesHandler struct {
	service      *service.NotesService
	maxBodyBytes int64 // upper bound on request body size (bytes)
}

// NewNotesHandler creates a handler wired to the given service.
// maxBodyBytes caps the request body size before decoding; requests
// exceeding this limit are rejected with HTTP 413 before any heap
// allocation for the body content occurs.
func NewNotesHandler(svc *service.NotesService, maxBodyBytes int64) *NotesHandler {
	return &NotesHandler{service: svc, maxBodyBytes: maxBodyBytes}
}

// ── GET /notes ───────────────────────────────────────────────────────────────

// ListNotes returns a paginated list of notes.
// The generated wrapper parses ?after=, ?limit=, and ?sort= into params for us.
// Handler responsibility: call the service with the pagination/sort params and
// map the PaginatedNotes result → gen.NoteList (the API envelope).
func (h *NotesHandler) ListNotes(w http.ResponseWriter, r *http.Request, params gen.ListNotesParams) {
	result, err := h.service.ListNotes(r.Context(), params.After, string(params.Sort), params.Limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Map domain notes → API notes, then build the NoteList envelope.
	// NextCursor is only included when HasMore is true, so clients know
	// there's a next page and what cursor to pass as ?after=.
	data := make([]gen.Note, 0, len(result.Data))
	for _, note := range result.Data {
		data = append(data, toAPINote(note))
	}

	response := gen.NoteList{
		Data:    data,
		Limit:   result.Limit,
		HasMore: result.HasMore,
	}
	if result.HasMore {
		response.NextCursor = result.NextCursor
	}

	writeJSON(w, http.StatusOK, response)
}

// ── POST /notes ──────────────────────────────────────────────────────────────

// CreateNote parses the JSON body and delegates to the service.
// The handler parses HTTP; the service validates and computes derived fields.
func (h *NotesHandler) CreateNote(w http.ResponseWriter, r *http.Request) {
	// Cap body size before any read so an oversized payload is rejected
	// immediately rather than being buffered into heap memory.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer r.Body.Close()

	// Parse the JSON request body into gen.CreateNoteJSONRequestBody.
	// This type is auto-generated from the OpenAPI spec and is a type alias
	// for gen.NewNote, which has a single field: Content string `json:"content"`.
	var body gen.CreateNoteJSONRequestBody

	decoder := json.NewDecoder(r.Body)

	// DisallowUnknownFields() makes the decoder reject JSON with extra fields.
	// For example, {"content": "hello", "extra": 123} would be rejected.
	// This is a defensive practice — it prevents clients from sending
	// unexpected data that would be silently ignored.
	decoder.DisallowUnknownFields()

	// Decode reads the JSON from the stream and fills the body struct.
	// If the JSON is malformed or contains unknown fields, it returns an error
	// and we respond with HTTP 400 Bad Request.
	// If the body exceeds maxBodyBytes, MaxBytesReader causes Decode to
	// return *http.MaxBytesError — we respond with HTTP 413.
	if err := decoder.Decode(&body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}

	// Delegate to service — it will validate, trim, derive title, count words…
	note, err := h.service.CreateNote(r.Context(), body.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toAPINote(note))
}

// ── GET /notes/{id} ──────────────────────────────────────────────────────────

// GetNote retrieves a single note by its ID.
// The generated wrapper extracts and parses the {id} path parameter for us.
func (h *NotesHandler) GetNote(w http.ResponseWriter, r *http.Request, id int) {
	note, err := h.service.GetNote(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPINote(note))
}

// ── PUT /notes/{id} ──────────────────────────────────────────────────────────

// UpdateNote parses the JSON body and delegates to the service.
// The generated wrapper extracts and parses the {id} path parameter for us.
func (h *NotesHandler) UpdateNote(w http.ResponseWriter, r *http.Request, id int) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer r.Body.Close()

	var body gen.UpdateNoteJSONRequestBody
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}

	note, err := h.service.UpdateNote(r.Context(), id, body.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toAPINote(note))
}

// ── Helper functions ─────────────────────────────────────────────────────────

// toAPINote maps a single service.Note (domain) → gen.Note (API JSON).
// Used for single-note responses (GetNote, CreateNote, UpdateNote).
// Paginated list responses use gen.NoteList, which wraps a []gen.Note
// built by calling this function per element in ListNotes.
func toAPINote(n service.Note) gen.Note {
	return gen.Note{
		Id:        n.ID,
		Content:   n.Content,
		Title:     n.Title,
		WordCount: n.WordCount,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

// writeServiceError maps service-layer sentinel errors to HTTP status codes.
// This is a great example of the handler's job: it doesn't know WHY the error
// happened (that's the service's domain), it just knows which HTTP status code
// to use for each error type.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidContent):
		writeError(w, http.StatusBadRequest, service.ErrInvalidContent.Error())
	case errors.Is(err, service.ErrContentTooLong):
		writeError(w, http.StatusBadRequest, service.ErrContentTooLong.Error())
	case errors.Is(err, service.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, service.ErrInvalidCursor.Error())
	case errors.Is(err, service.ErrInvalidSort):
		writeError(w, http.StatusBadRequest, service.ErrInvalidSort.Error())
	case errors.Is(err, service.ErrInvalidLimit):
		writeError(w, http.StatusBadRequest, service.ErrInvalidLimit.Error())
	case errors.Is(err, service.ErrNoteNotFound):
		writeError(w, http.StatusNotFound, service.ErrNoteNotFound.Error())
	default:
		log.Printf("ERROR: unhandled service error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// writeJSON sends a JSON response with the given status code and payload.
// This centralizes HTTP response handling, keeping it separate from business logic.
func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	// Encode into a buffer first so that a serialization error does not corrupt
	// a partially-written response (headers have not been sent yet at this point).
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// writeError is a convenience wrapper that sends a JSON error response.
// It wraps the message in {"error": message} for consistent error formatting.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
