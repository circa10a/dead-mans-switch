package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/circa10a/dead-mans-switch/api"
)

// AuthConfigHandler serves the given auth configuration so the UI can discover OIDC settings.
func AuthConfigHandler(cfg api.AuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}
