// Package middleware provides lightweight transport-level request validation.
//
// Why this package exists when strict server mode is enabled:
// strict mode gives typed request objects and response contracts, but it does
// not automatically reproduce all legacy transport guards we used previously
// (for example unknown query key rejection and explicit body size caps).
//
// These middlewares restore those targeted behaviors without loading embedded
// OpenAPI specs at runtime and without adding router-specific dependencies.
package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// EnforceBodyAndContentType applies body-size and content-type checks for write operations.
func EnforceBodyAndContentType(maxBodyBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only POST /notes and PUT /notes/{id} accept JSON bodies.
			if !isWriteNotesOperation(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Accept "application/json" with optional parameters (e.g. charset=utf-8).
			// Any parse error or different media type is rejected with 415.
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeJSONError(w, http.StatusUnsupportedMediaType, "unsupported media type")
				return
			}

			// Wrap request body with a hard byte limit. If downstream reads past the
			// limit, Go returns *http.MaxBytesError which another middleware maps to 413.
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// RejectUnknownQueryParams rejects query keys that are not allowed for an operation.
func RejectUnknownQueryParams() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Determine whether current method+path is one we validate.
			allowed, ok := allowedQueryParamsForOperation(r.Method, r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Collect unknown keys so response is deterministic and testable.
			query := r.URL.Query()
			unknown := make([]string, 0, len(query))
			for key := range query {
				if _, exists := allowed[key]; !exists {
					unknown = append(unknown, key)
				}
			}
			if len(unknown) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Match previous behavior: report lexicographically first unknown key.
			sort.Strings(unknown)
			writeJSONError(w, http.StatusBadRequest, "unknown query parameter: "+unknown[0])
		})
	}
}

// EnforceQueryRules validates operation-specific query constraints that strict
// binding alone does not enforce (for example string length and value ranges).
func EnforceQueryRules(maxAfterLength, maxLimit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isListNotesOperation(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			query := r.URL.Query()

			// "after" is optional, but when present it must be non-empty and bounded.
			if values, ok := query["after"]; ok && len(values) > 0 {
				after := values[0]
				if after == "" || len(after) > maxAfterLength {
					writeJSONError(w, http.StatusBadRequest, "invalid cursor")
					return
				}
			}

			// "limit" is optional, but when present it must be within [1, maxLimit].
			if values, ok := query["limit"]; ok && len(values) > 0 {
				limit, err := strconv.Atoi(values[0])
				if err != nil || limit < 1 || limit > maxLimit {
					writeJSONError(w, http.StatusBadRequest, "invalid limit parameter")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RejectUnknownJSONFields ensures NewNote payloads only contain known fields.
func RejectUnknownJSONFields() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only write operations carry NewNote payloads.
			if !isWriteNotesOperation(r.Method, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Read body once to inspect keys. Body is already size-limited by the
			// outer middleware, so this cannot grow without bound.
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					// Explicit parity behavior for oversized payloads.
					writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
					return
				}
				// Transport read failures are treated as invalid JSON body.
				writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}

			valid, malformed := validateNewNoteObject(raw)
			if malformed {
				// Avoid duplicate malformed-json handling. We restore the body and let
				// strict generated decoding return the request error response.
				r.Body = io.NopCloser(bytes.NewReader(raw))
				r.ContentLength = int64(len(raw))
				next.ServeHTTP(w, r)
				return
			}
			if !valid {
				writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}

			// Restore body for strict generated decoder that runs downstream.
			r.Body = io.NopCloser(bytes.NewReader(raw))
			r.ContentLength = int64(len(raw))
			next.ServeHTTP(w, r)
		})
	}
}

// validateNewNoteObject checks whether payload is a single JSON object with no
// unknown top-level keys for NewNote.
//
// Return values:
//   - valid=true, malformed=false  -> object shape acceptable for this middleware
//   - valid=false, malformed=false -> well-formed JSON object but has unknown keys
//   - valid=false, malformed=true  -> malformed JSON / non-object JSON payload
func validateNewNoteObject(raw []byte) (valid, malformed bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))

	var payload map[string]json.RawMessage
	if err := dec.Decode(&payload); err != nil {
		return false, true
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false, true
	}

	// JSON "null" unmarshals into a nil map here. Treat this as malformed for
	// the NewNote object shape expected by this middleware.
	if payload == nil {
		return false, true
	}

	for key := range payload {
		if key != "content" {
			return false, false
		}
	}

	return true, false
}

// allowedQueryParamsForOperation returns the query key allowlist for known
// operations. bool=false means "not handled by this middleware".
func allowedQueryParamsForOperation(method, path string) (map[string]struct{}, bool) {
	switch {
	case method == http.MethodGet && path == "/notes":
		return map[string]struct{}{"after": {}, "limit": {}, "sort": {}}, true
	case method == http.MethodPost && path == "/notes":
		return map[string]struct{}{}, true
	case method == http.MethodGet && isNotesIDPath(path):
		return map[string]struct{}{}, true
	case method == http.MethodPut && isNotesIDPath(path):
		return map[string]struct{}{}, true
	default:
		return nil, false
	}
}

// isWriteNotesOperation identifies operations that consume NewNote JSON bodies.
func isWriteNotesOperation(method, path string) bool {
	if method == http.MethodPost && path == "/notes" {
		return true
	}
	if method == http.MethodPut && isNotesIDPath(path) {
		return true
	}
	return false
}

// isListNotesOperation identifies GET /notes.
func isListNotesOperation(method, path string) bool {
	return method == http.MethodGet && path == "/notes"
}

// isNotesIDPath matches /notes/{id} shape (one non-empty path segment after /notes/).
func isNotesIDPath(path string) bool {
	if !strings.HasPrefix(path, "/notes/") {
		return false
	}

	rest := strings.TrimPrefix(path, "/notes/")
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	return true
}

// writeJSONError writes the API-standard error payload.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
