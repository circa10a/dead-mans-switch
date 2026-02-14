package handlers

import (
	"encoding/json"
	"net/http"
)

// authConfigResponse represents the auth configuration returned to the UI.
type authConfigResponse struct {
	Audience  string `json:"audience,omitempty"`
	Enabled   bool   `json:"enabled"`
	IssuerURL string `json:"issuerUrl,omitempty"`
}

// Auth handles authentication configuration requests.
type Auth struct {
	Audience  string
	Enabled   bool
	IssuerURL string
}

// GetConfigHandleFunc returns the auth configuration so the UI can discover OIDC settings.
func (a *Auth) GetConfigHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := authConfigResponse{
		Enabled: a.Enabled,
	}

	if a.Enabled {
		resp.Audience = a.Audience
		resp.IssuerURL = a.IssuerURL
	}

	_ = json.NewEncoder(w).Encode(resp)
}
