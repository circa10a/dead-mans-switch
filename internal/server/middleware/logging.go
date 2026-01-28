package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter is a wrapper for http.ResponseWriter for custom logging fields
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
	rw.wroteHeader = true
}

// Logging wraps an http.Handler for access logging.
func Logging(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service := "http"
		wrapped := wrapResponseWriter(w)
		startTime := time.Now()
		next.ServeHTTP(wrapped, r)

		remoteAddr := r.Header.Get("x-forwarded-for")
		if remoteAddr == "" {
			remoteAddr = r.RemoteAddr
		}

		fields := []any{
			"status", wrapped.status,
			"method", r.Method,
			"duration", time.Since(startTime).String(),
			"ip", remoteAddr,
			"path", r.RequestURI,
		}

		switch wrapped.status {
		case http.StatusInternalServerError:
			l.Error(service, fields...)
		case http.StatusNotFound:
			l.Warn(service, fields...)
		default:
			l.Info(service, fields...)
		}
	})
}
