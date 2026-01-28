package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// Error messages
const (
	errInvalidSwitchID      = "Invalid switch ID"
	errSwitchNotFound       = "Switch not found"
	errDatabaseError        = "Database error"
	errFailedToDelete       = "Failed to delete switch"
	errFailedToReset        = "Failed to reset switch"
	errFailedToFetchUpdated = "Error fetching updated switch"
)

const defaultLimit = 100

// Switch handles dead man switch requests.
type Switch struct {
	Validator *validator.Validate
	Store     *database.Store
	Logger    *slog.Logger
}

// PostHandleFunc creates a dead mans switch.
func (s *Switch) PostHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	payload := api.Switch{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err), err)
		return
	}
	if err := s.Validator.Struct(payload); err != nil {
		s.sendError(w, http.StatusBadRequest, "Validation failed", err)
		return
	}

	createdSwitch, err := s.Store.Create(payload)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createdSwitch)
}

// GetHandleFunc retrieves all switches, optionally filtered by the "sent" status.
func (s *Switch) GetHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	limit := defaultLimit

	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	var foundSwitches []api.Switch
	var err error

	sentRaw := r.URL.Query().Get("sent")

	if sentRaw != "" {
		sentBool, err := strconv.ParseBool(sentRaw)
		if err != nil {
			s.sendError(w, http.StatusBadRequest, "Invalid value for 'sent' parameter. Use 'true' or 'false'.", err)
			return
		}
		foundSwitches, err = s.Store.GetAllBySent(sentBool, limit)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
			return
		}
	} else {
		foundSwitches, err = s.Store.GetAll(limit)
	}

	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	if foundSwitches == nil {
		foundSwitches = []api.Switch{}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(foundSwitches)
}

// GetByIDHandleFunc retrieves a single switch by its ID.
func (s *Switch) GetByIDHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	foundSwitch, err := s.Store.GetByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(foundSwitch)
}

// DeleteHandleFunc deletes a switch.
func (s *Switch) DeleteHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	deletedSwitch, err := s.Store.GetByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	if err := s.Store.Delete(id); err != nil {
		s.sendError(w, http.StatusInternalServerError, errFailedToDelete, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(deletedSwitch)
}

// ResetHandleFunc resets the dead man switch timer.
func (s *Switch) ResetHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	if err := s.Store.Reset(id); err != nil {
		if err == sql.ErrNoRows {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errFailedToReset, err)
		return
	}

	updatedSwitch, err := s.Store.GetByID(id)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errFailedToFetchUpdated, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(updatedSwitch)
}

// sendError handles both the JSON response and logging of internal errors
func (s *Switch) sendError(w http.ResponseWriter, code int, publicMsg string, internalErr error) {
	// Only log as Error if it's a 500+ status code
	if code >= http.StatusInternalServerError {
		s.Logger.Error(publicMsg, "error", internalErr)
	}

	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(api.Error{
		Code:    code,
		Message: publicMsg,
	})
}
