package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthGetConfigHandleFunc(t *testing.T) {
	tests := []struct {
		name             string
		handler          *Auth
		expectedAudience string
		expectedEnabled  bool
		expectedIssuer   string
	}{
		{
			name: "auth disabled",
			handler: &Auth{
				Enabled: false,
			},
			expectedEnabled: false,
		},
		{
			name: "auth enabled with issuer and audience",
			handler: &Auth{
				Audience:  "my-client-id",
				Enabled:   true,
				IssuerURL: "http://localhost:9000/application/o/dead-mans-switch/",
			},
			expectedAudience: "my-client-id",
			expectedEnabled:  true,
			expectedIssuer:   "http://localhost:9000/application/o/dead-mans-switch/",
		},
		{
			name: "auth enabled without audience",
			handler: &Auth{
				Enabled:   true,
				IssuerURL: "http://localhost:9000/application/o/dead-mans-switch/",
			},
			expectedEnabled: true,
			expectedIssuer:  "http://localhost:9000/application/o/dead-mans-switch/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil)
			rec := httptest.NewRecorder()

			tt.handler.GetConfigHandleFunc(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			var resp authConfigResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Enabled != tt.expectedEnabled {
				t.Errorf("expected enabled=%v, got %v", tt.expectedEnabled, resp.Enabled)
			}

			if resp.IssuerURL != tt.expectedIssuer {
				t.Errorf("expected issuerUrl=%q, got %q", tt.expectedIssuer, resp.IssuerURL)
			}

			if resp.Audience != tt.expectedAudience {
				t.Errorf("expected audience=%q, got %q", tt.expectedAudience, resp.Audience)
			}
		})
	}
}
