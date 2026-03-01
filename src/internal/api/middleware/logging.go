package middleware

import (
	"net/http"
	"time"

	"notes-api/internal/logx"
)

// RequestLogger logs one line per HTTP request with status and duration.
// It intentionally avoids logging request/response bodies.
func RequestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			level := "INFO"
			switch {
			case rec.status >= 500:
				level = "ERROR"
			case rec.status >= 400:
				level = "WARN"
			}

			reqID := r.Header.Get("X-Request-Id")
			if reqID == "" {
				reqID = "-"
			}

			switch level {
			case "ERROR":
				logx.Errorf(
					"msg=%q method=%s path=%s status=%d bytes=%d duration_ms=%d remote=%q ua=%q req_id=%q",
					"http_request",
					r.Method,
					r.URL.Path,
					rec.status,
					rec.bytes,
					time.Since(start).Milliseconds(),
					r.RemoteAddr,
					r.UserAgent(),
					reqID,
				)
			case "WARN":
				logx.Warnf(
					"msg=%q method=%s path=%s status=%d bytes=%d duration_ms=%d remote=%q ua=%q req_id=%q",
					"http_request",
					r.Method,
					r.URL.Path,
					rec.status,
					rec.bytes,
					time.Since(start).Milliseconds(),
					r.RemoteAddr,
					r.UserAgent(),
					reqID,
				)
			default:
				logx.Infof(
					"msg=%q method=%s path=%s status=%d bytes=%d duration_ms=%d remote=%q ua=%q req_id=%q",
					"http_request",
					r.Method,
					r.URL.Path,
					rec.status,
					rec.bytes,
					time.Since(start).Milliseconds(),
					r.RemoteAddr,
					r.UserAgent(),
					reqID,
				)
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}
