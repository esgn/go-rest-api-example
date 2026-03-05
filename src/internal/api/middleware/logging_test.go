package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRequestLogger_DefaultStatus200(t *testing.T) {
	var buf bytes.Buffer
	restore := swapSlogOutput(&buf)
	defer restore()

	h := RequestLogger()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/notes", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	out := buf.String()
	assertContains(t, out, "msg=http_request")
	assertContains(t, out, "method=GET")
	assertContains(t, out, "path=/notes")
	assertContains(t, out, "status=200")
	assertContains(t, out, "bytes=2")
	assertContains(t, out, "req_id=-")
}

func TestRequestLogger_StatusLevelAndRequestID(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantLevel    string
		setRequestID bool
	}{
		{name: "warn on 4xx", status: http.StatusBadRequest, wantLevel: "WARN"},
		{name: "error on 5xx", status: http.StatusInternalServerError, wantLevel: "ERROR"},
		{name: "info on 2xx with request id", status: http.StatusCreated, wantLevel: "INFO", setRequestID: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			restore := swapSlogOutput(&buf)
			defer restore()

			h := RequestLogger()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))

			req := httptest.NewRequest(http.MethodPost, "/notes", nil)
			if tc.setRequestID {
				req.Header.Set("X-Request-Id", "req-123")
			}

			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			out := buf.String()
			assertContains(t, out, "level="+tc.wantLevel)
			assertContains(t, out, "status="+strconv.Itoa(tc.status))
			if tc.setRequestID {
				assertContains(t, out, "req_id=req-123")
			} else {
				assertContains(t, out, "req_id=-")
			}
		})
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected log to contain %q, got %q", needle, haystack)
	}
}

func swapSlogOutput(buf *bytes.Buffer) func() {
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	return func() {
		slog.SetDefault(orig)
	}
}
