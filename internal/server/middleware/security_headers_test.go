package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeaders(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "X-Content-Type-Options is set",
			header:   "X-Content-Type-Options",
			expected: "nosniff",
		},
		{
			name:     "Cross-Origin-Resource-Policy is set",
			header:   "Cross-Origin-Resource-Policy",
			expected: "same-origin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rr.Header().Get(tt.header)
			if got != tt.expected {
				t.Errorf("expected %s=%q, got %q", tt.header, tt.expected, got)
			}
		})
	}
}
