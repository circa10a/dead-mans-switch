package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
)

// Handles health check requests.
type Health struct {
	Store database.Store
}

// GetHandleFunc handles health check requests by verifying the database connection.
func (h *Health) GetHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var status api.HealthStatus

	// Check if DB is healthy
	err := h.Store.Ping()
	if err != nil {
		status = api.Failed
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		status = api.Ok
		w.WriteHeader(http.StatusOK)
	}

	resp := api.Health{
		Status: status,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
