package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/circa10a/dead-mans-switch/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

// Error messages
const (
	errInvalidSwitchID = "Invalid switch ID"
	errTimeParse       = "Invalid time duration"
	errLimitValue      = "Invalid limit value"
	errSwitchNotFound  = "Switch not found"
	errDatabaseError   = "Database error"
	errFailedToDelete  = "Failed to delete switch"
	errFailedToReset   = "Failed to reset switch"
)

// Send all unless specified.
const defaultLimit = -1

// Switch handles dead man switch requests.
type Switch struct {
	Store  database.Store
	Logger *slog.Logger
}

// PostHandleFunc creates a dead mans switch.
func (s *Switch) PostHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	payload, ok := middleware.FromContext(r.Context())
	if !ok {
		s.sendError(w, http.StatusInternalServerError, "Internal context error", nil)
		return
	}

	duration, err := time.ParseDuration(payload.CheckInInterval)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errTimeParse, err)
		return
	}
	// Compute time at which to send
	sendAt := time.Now().Add(duration).Unix()
	payload.SendAt = &sendAt

	// If push subscription and threshold defined, set reminderEnabled to true
	reminderEnabled := payload.PushSubscription != nil && payload.ReminderThreshold != nil && *payload.ReminderThreshold != ""
	payload.ReminderEnabled = &reminderEnabled

	createdSwitch, err := s.Store.Create(payload)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(s.redact(createdSwitch))
}

// GetHandleFunc retrieves all switches, optionally filtered by the "sent" status.
func (s *Switch) GetHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	limit := defaultLimit

	if l := r.URL.Query().Get("limit"); l != "" {
		val, err := strconv.Atoi(l)
		if err != nil && val > 0 {
			s.sendError(w, http.StatusBadRequest, errLimitValue, err)
			return
		}
		limit = val
	}

	foundSwitches, err := s.Store.GetAll(limit)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	if foundSwitches == nil {
		foundSwitches = []api.Switch{}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.redactAll(foundSwitches))
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
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.redact(foundSwitch))
}

// PutByIDHandleFunc updates a single switch by its ID.
func (s *Switch) PutByIDHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	payload, ok := middleware.FromContext(r.Context())
	if !ok {
		s.sendError(w, http.StatusInternalServerError, "Internal context error", nil)
		return
	}

	// Get existing checkInInterval, if changed, set the new time
	previousSwitch, err := s.Store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	// Keep existing send time
	payload.SendAt = previousSwitch.SendAt

	// Change send time if checkInInterval changed
	if previousSwitch.CheckInInterval != payload.CheckInInterval {
		duration, err := time.ParseDuration(payload.CheckInInterval)
		if err != nil {
			s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
			return
		}
		updatedSendAt := time.Now().Add(duration).Unix()
		payload.SendAt = &updatedSendAt
	}

	// If push subscription and threshold defined, set reminderEnabled to true
	reminderEnabled := payload.PushSubscription != nil && payload.ReminderThreshold != nil && *payload.ReminderThreshold != ""
	payload.ReminderEnabled = &reminderEnabled

	updatedSwitch, err := s.Store.Update(id, payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, nil)
			return
		}
		s.sendError(w, http.StatusInternalServerError, "Failed to update switch", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.redact(updatedSwitch))
}

// DeleteHandleFunc deletes a switch.
func (s *Switch) DeleteHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	_, err = s.Store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	err = s.Store.Delete(id)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, errFailedToDelete, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ResetHandleFunc resets the dead man switch timer.
func (s *Switch) ResetHandleFunc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errInvalidSwitchID, err)
		return
	}

	var req struct {
		PushSubscription *api.PushSubscription `json:"pushSubscription"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	switchToReset, err := s.Store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	duration, err := time.ParseDuration(switchToReset.CheckInInterval)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, errTimeParse, err)
		return
	}

	// Update sendAt time
	newSendAt := time.Now().UTC().Add(duration).Unix()
	switchToReset.SendAt = &newSendAt

	// Set default values to false
	defaultOff := false
	switchToReset.Disabled = &defaultOff
	switchToReset.Sent = &defaultOff
	switchToReset.ReminderSent = &defaultOff

	resetSwitch, err := s.Store.Update(id, switchToReset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errFailedToReset, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.redact(resetSwitch))
}

// DisableHandleFunc marks a switch as disabled.
func (s *Switch) DisableHandleFunc(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid switch ID", err)
		return
	}

	switchToDisable, err := s.Store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errDatabaseError, err)
		return
	}

	v := true
	switchToDisable.Disabled = &v

	disabledSwitch, err := s.Store.Update(id, switchToDisable)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.sendError(w, http.StatusNotFound, errSwitchNotFound, err)
			return
		}
		s.sendError(w, http.StatusInternalServerError, errFailedToReset, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.redact(disabledSwitch))
}

// sendError handles both the JSON response and logging of internal errors
func (s *Switch) sendError(w http.ResponseWriter, code int, publicMsg string, internalErr error) {
	if code >= http.StatusInternalServerError {
		s.Logger.Error(publicMsg, "error", internalErr)
	}

	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(api.Error{
		Code:    code,
		Message: publicMsg,
	})
}

// redact removes sensitive push subscription details before sending to the client.
func (s *Switch) redact(sw api.Switch) api.Switch {
	sw.PushSubscription = nil

	return sw
}

// redactAll returns a new slice with redacted push subscription data.
func (s *Switch) redactAll(switches []api.Switch) []api.Switch {
	redacted := make([]api.Switch, len(switches))
	for i, sw := range switches {
		redacted[i] = s.redact(sw)
	}
	return redacted
}
