package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
)

func TestAuthGetConfigHandleFunc(t *testing.T) {
	audience := "my-client-id"
	issuer := "http://localhost:9000/application/o/dead-mans-switch/"

	tests := []struct {
		name             string
		cfg              api.AuthConfig
		expectedAudience string
		expectedEnabled  bool
		expectedIssuer   string
	}{
		{
			name: "auth disabled",
			cfg: api.AuthConfig{
				Enabled: false,
			},
			expectedEnabled: false,
		},
		{
			name: "auth enabled with issuer and audience",
			cfg: api.AuthConfig{
				Audience:  &audience,
				Enabled:   true,
				IssuerUrl: &issuer,
			},
			expectedAudience: "my-client-id",
			expectedEnabled:  true,
			expectedIssuer:   "http://localhost:9000/application/o/dead-mans-switch/",
		},
		{
			name: "auth enabled without audience",
			cfg: api.AuthConfig{
				Enabled:   true,
				IssuerUrl: &issuer,
			},
			expectedEnabled: true,
			expectedIssuer:  "http://localhost:9000/application/o/dead-mans-switch/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil)
			rec := httptest.NewRecorder()

			handler := AuthConfigHandler(tt.cfg)
			handler(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			var resp api.AuthConfig
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Enabled != tt.expectedEnabled {
				t.Errorf("expected enabled=%v, got %v", tt.expectedEnabled, resp.Enabled)
			}

			issuerUrl := ""
			if resp.IssuerUrl != nil {
				issuerUrl = *resp.IssuerUrl
			}
			if issuerUrl != tt.expectedIssuer {
				t.Errorf("expected issuerUrl=%q, got %q", tt.expectedIssuer, issuerUrl)
			}

			audience := ""
			if resp.Audience != nil {
				audience = *resp.Audience
			}
			if audience != tt.expectedAudience {
				t.Errorf("expected audience=%q, got %q", tt.expectedAudience, audience)
			}
		})
	}
}
