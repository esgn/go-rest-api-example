// Package api contains HTTP handlers that translate between generated strict
// OpenAPI request/response types and the service layer domain API.
//
// Responsibilities of this layer:
//   - Read already-decoded strict request objects
//   - Call service use-case methods
//   - Map service errors to HTTP response objects
//   - Convert service.Note <-> generated API models
//
// Non-responsibilities:
//   - No persistence concerns (repository/db own that)
//   - No core business rules (service owns that)
package api

import (
	"context"
	"errors"
	"log/slog"

	gen "notes-api/internal/http/openapi"
	"notes-api/internal/notes/service"
)

// this compile-time assertion ensures NotesHandler implements the strict server interface
var _ gen.StrictServerInterface = (*NotesHandler)(nil)

// NotesHandler translates strict OpenAPI request/response objects to service calls.
type NotesHandler struct {
	service *service.NotesService
}

// NewNotesHandler builds a strict-server-compatible handler implementation.
func NewNotesHandler(svc *service.NotesService) *NotesHandler {
	return &NotesHandler{service: svc}
}

// ListNotes handles GET /notes.
//
// Transport/schema checks are handled by middleware and strict binders.
// This method focuses on translating typed request inputs to service calls.
func (h *NotesHandler) ListNotes(ctx context.Context, request gen.ListNotesRequestObject) (gen.ListNotesResponseObject, error) {
	// Convert optional strict params into service input primitives.
	after := ""
	if request.Params.After != nil {
		after = *request.Params.After
	}

	limit := 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}

	sort := ""
	if request.Params.Sort != nil {
		sort = string(*request.Params.Sort)
	}

	// Service performs cursor decoding, sort validation, limit clamping, and fetch.
	result, err := h.service.ListNotes(ctx, after, sort, limit)
	if err != nil {
		return listNotesErrorResponse(err, after, sort, limit), nil
	}

	// Convert domain notes to generated API notes.
	data := make([]gen.Note, 0, len(result.Data))
	for _, note := range result.Data {
		data = append(data, toAPINote(note))
	}

	// Build API envelope and include next_cursor only when present.
	response := gen.NoteList{
		Data:    data,
		Limit:   result.Limit,
		HasMore: result.HasMore,
	}

	if result.HasMore && result.NextCursor != "" {
		cursor := result.NextCursor
		response.NextCursor = &cursor
	}

	return gen.ListNotes200JSONResponse(response), nil
}

// CreateNote handles POST /notes.
//
// Body decoding/typing is performed by strict wrappers before this method.
// We still guard against nil body for robustness and consistent 400 payload.
func (h *NotesHandler) CreateNote(ctx context.Context, request gen.CreateNoteRequestObject) (gen.CreateNoteResponseObject, error) {
	if request.Body == nil {
		return gen.CreateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: "invalid JSON body"},
		}, nil
	}

	note, err := h.service.CreateNote(ctx, request.Body.Content)
	if err != nil {
		return createNoteErrorResponse(err), nil
	}

	return gen.CreateNote201JSONResponse(toAPINote(note)), nil
}

// GetNote handles GET /notes/{id}.
func (h *NotesHandler) GetNote(ctx context.Context, request gen.GetNoteRequestObject) (gen.GetNoteResponseObject, error) {
	note, err := h.service.GetNote(ctx, request.Id)
	if err != nil {
		return getNoteErrorResponse(request.Id, err), nil
	}

	return gen.GetNote200JSONResponse(toAPINote(note)), nil
}

// UpdateNote handles PUT /notes/{id}.
//
// Similar to CreateNote, strict decoding has already happened. The handler
// only translates to service inputs and maps service errors back to transport.
func (h *NotesHandler) UpdateNote(ctx context.Context, request gen.UpdateNoteRequestObject) (gen.UpdateNoteResponseObject, error) {
	if request.Body == nil {
		return gen.UpdateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: "invalid JSON body"},
		}, nil
	}

	note, err := h.service.UpdateNote(ctx, request.Id, request.Body.Content)
	if err != nil {
		return updateNoteErrorResponse(request.Id, err), nil
	}

	return gen.UpdateNote200JSONResponse(toAPINote(note)), nil
}

// toAPINote maps the service/domain model to the generated OpenAPI response model.
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

// badRequestList is a tiny helper to keep list 400 responses consistent.
func badRequestList(message string) gen.ListNotesResponseObject {
	return gen.ListNotes400JSONResponse{
		BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: message},
	}
}

// listNotesErrorResponse maps service-layer errors to ListNotes HTTP responses.
func listNotesErrorResponse(err error, after, sort string, limit int) gen.ListNotesResponseObject {
	switch {
	case errors.Is(err, service.ErrInvalidCursor):
		return badRequestList(service.ErrInvalidCursor.Error())
	case errors.Is(err, service.ErrInvalidSort):
		return badRequestList(service.ErrInvalidSort.Error())
	case errors.Is(err, service.ErrInvalidLimit):
		return badRequestList(service.ErrInvalidLimit.Error())
	default:
		slog.Error("unhandled_service_error",
			"operation", "ListNotes",
			"after_present", after != "",
			"sort", sort,
			"limit", limit,
			"err", err.Error(),
		)
		return gen.ListNotes500JSONResponse{
			InternalServerErrorJSONResponse: gen.InternalServerErrorJSONResponse{Error: "internal server error"},
		}
	}
}

// createNoteErrorResponse maps service-layer errors to CreateNote HTTP responses.
func createNoteErrorResponse(err error) gen.CreateNoteResponseObject {
	switch {
	case errors.Is(err, service.ErrInvalidContent):
		return gen.CreateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: service.ErrInvalidContent.Error()},
		}
	case errors.Is(err, service.ErrContentTooLong):
		return gen.CreateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: service.ErrContentTooLong.Error()},
		}
	default:
		slog.Error("unhandled_service_error",
			"operation", "CreateNote",
			"err", err.Error(),
		)
		return gen.CreateNote500JSONResponse{
			InternalServerErrorJSONResponse: gen.InternalServerErrorJSONResponse{Error: "internal server error"},
		}
	}
}

// getNoteErrorResponse maps service-layer errors to GetNote HTTP responses.
func getNoteErrorResponse(id int, err error) gen.GetNoteResponseObject {
	switch {
	case errors.Is(err, service.ErrNoteNotFound):
		return gen.GetNote404JSONResponse{
			NotFoundJSONResponse: gen.NotFoundJSONResponse{Error: service.ErrNoteNotFound.Error()},
		}
	default:
		slog.Error("unhandled_service_error",
			"operation", "GetNote",
			"note_id", id,
			"err", err.Error(),
		)
		return gen.GetNote500JSONResponse{
			InternalServerErrorJSONResponse: gen.InternalServerErrorJSONResponse{Error: "internal server error"},
		}
	}
}

// updateNoteErrorResponse maps service-layer errors to UpdateNote HTTP responses.
func updateNoteErrorResponse(id int, err error) gen.UpdateNoteResponseObject {
	switch {
	case errors.Is(err, service.ErrInvalidContent):
		return gen.UpdateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: service.ErrInvalidContent.Error()},
		}
	case errors.Is(err, service.ErrContentTooLong):
		return gen.UpdateNote400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Error: service.ErrContentTooLong.Error()},
		}
	case errors.Is(err, service.ErrNoteNotFound):
		return gen.UpdateNote404JSONResponse{
			NotFoundJSONResponse: gen.NotFoundJSONResponse{Error: service.ErrNoteNotFound.Error()},
		}
	default:
		slog.Error("unhandled_service_error",
			"operation", "UpdateNote",
			"note_id", id,
			"err", err.Error(),
		)
		return gen.UpdateNote500JSONResponse{
			InternalServerErrorJSONResponse: gen.InternalServerErrorJSONResponse{Error: "internal server error"},
		}
	}
}
