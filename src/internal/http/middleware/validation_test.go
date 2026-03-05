package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errorBody struct {
	Error string `json:"error"`
}

func TestValidationMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method         string
		target         string
		contentType    string
		body           string
		maxBodyBytes   int64
		maxAfterLen    int
		maxLimit       int
		expectedStatus int
		expectedError  string
		expectReached  bool
	}{
		{
			name:           "get notes rejects unknown query",
			method:         http.MethodGet,
			target:         "/notes?foo=1",
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "unknown query parameter: foo",
		},
		{
			name:           "post notes rejects unknown query",
			method:         http.MethodPost,
			target:         "/notes?foo=1",
			contentType:    "application/json",
			body:           `{"content":"x"}`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "unknown query parameter: foo",
		},
		{
			name:           "get notes rejects empty after",
			method:         http.MethodGet,
			target:         "/notes?after=",
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid cursor",
		},
		{
			name:           "get notes rejects too-long after",
			method:         http.MethodGet,
			target:         "/notes?after=" + strings.Repeat("a", 513),
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid cursor",
		},
		{
			name:           "get notes rejects zero limit",
			method:         http.MethodGet,
			target:         "/notes?limit=0",
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid limit parameter",
		},
		{
			name:           "get notes rejects over-max limit",
			method:         http.MethodGet,
			target:         "/notes?limit=101",
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid limit parameter",
		},
		{
			name:           "post notes rejects unsupported media type",
			method:         http.MethodPost,
			target:         "/notes",
			contentType:    "text/plain",
			body:           `{"content":"x"}`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusUnsupportedMediaType,
			expectedError:  "unsupported media type",
		},
		{
			name:           "put note rejects unsupported media type",
			method:         http.MethodPut,
			target:         "/notes/1",
			contentType:    "text/plain",
			body:           `{"content":"x"}`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusUnsupportedMediaType,
			expectedError:  "unsupported media type",
		},
		{
			name:           "post notes rejects oversized body",
			method:         http.MethodPost,
			target:         "/notes",
			contentType:    "application/json",
			body:           `{"content":"this body is larger than ten bytes"}`,
			maxBodyBytes:   10,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusRequestEntityTooLarge,
			expectedError:  "request body too large",
		},
		{
			name:           "post notes rejects unknown json field",
			method:         http.MethodPost,
			target:         "/notes",
			contentType:    "application/json",
			body:           `{"content":"x","extra":1}`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid JSON body",
		},
		{
			name:           "post notes malformed json passes through",
			method:         http.MethodPost,
			target:         "/notes",
			contentType:    "application/json",
			body:           `{invalid`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusNoContent,
			expectReached:  true,
		},
		{
			name:           "valid post passes through",
			method:         http.MethodPost,
			target:         "/notes",
			contentType:    "application/json",
			body:           `{"content":"x"}`,
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusNoContent,
			expectReached:  true,
		},
		{
			name:           "valid get with query passes through",
			method:         http.MethodGet,
			target:         "/notes?limit=10&sort=id",
			maxBodyBytes:   1024,
			maxAfterLen:    512,
			maxLimit:       100,
			expectedStatus: http.StatusNoContent,
			expectReached:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reached := false
			downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true

				if isWriteNotesOperation(r.Method, r.URL.Path) {
					b, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatalf("downstream read body: %v", err)
					}
					if len(b) == 0 {
						t.Fatal("downstream expected non-empty body")
					}
				}

				w.WriteHeader(http.StatusNoContent)
			})

			h := chain(
				downstream,
				EnforceBodyAndContentType(tc.maxBodyBytes),
				RejectUnknownQueryParams(),
				EnforceQueryRules(tc.maxAfterLen, tc.maxLimit),
				RejectUnknownJSONFields(),
			)

			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rr := httptest.NewRecorder()

			h.ServeHTTP(rr, req)

			if rr.Code != tc.expectedStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tc.expectedStatus)
			}
			if reached != tc.expectReached {
				t.Fatalf("downstream reached = %v, want %v", reached, tc.expectReached)
			}

			if tc.expectedError != "" {
				var body errorBody
				if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body.Error != tc.expectedError {
					t.Fatalf("error body = %q, want %q", body.Error, tc.expectedError)
				}
			}
		})
	}
}

func chain(base http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	h := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
