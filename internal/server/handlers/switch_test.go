package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/circa10a/dead-mans-switch/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// setupTestHandler initializes a Switch handler with a temporary database
func setupTestHandler(t *testing.T) (*Switch, database.Store) {
	t.Helper()

	store, err := database.New(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	err = store.Init()
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	s := &Switch{
		Store:  store,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}

	return s, store
}

func TestPostHandleFunc(t *testing.T) {
	s, _ := setupTestHandler(t)
	v := validator.New()
	mw := middleware.SwitchValidator(v)
	// Wrap the handler
	handlerToTest := mw(http.HandlerFunc(s.PostHandleFunc))

	t.Run("successfully creates a switch with message", func(t *testing.T) {
		payload := api.Switch{
			Message:         "Secret Message",
			Notifiers:       []string{"logger://", "discord://token@id"},
			CheckInInterval: "24h",
			DeleteAfterSent: true,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to unmarshal switch: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode switch: %v", err)
		}

		if resp.Message != payload.Message {
			t.Errorf("expected message %s, got %s", payload.Message, resp.Message)
		}

		if !reflect.DeepEqual(resp.Notifiers, payload.Notifiers) {
			t.Errorf("expected notifier %v, got %v", payload.Notifiers, resp.Notifiers)
		}
	})

	t.Run("returns 400 for empty message (validation check)", func(t *testing.T) {
		payload := api.Switch{
			Message:         "", // Fails validation because of OpenAPI/Validator "required,min=1"
			Notifiers:       []string{"logger://"},
			CheckInInterval: "24h",
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to unmarshal switch: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty message, got %d", rec.Code)
		}
	})

	t.Run("returns encrypted message and notifiers in response", func(t *testing.T) {
		plaintextNotifier := "discord://webhook-url"
		plaintextMessage := "Top Secret Message"

		payload := map[string]interface{}{
			"message":         plaintextMessage,
			"notifiers":       []string{plaintextNotifier},
			"checkInInterval": "24h",
			"encrypted":       true,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to unmarshal switch: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		response := api.Switch{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Check Message Encryption
		if response.Message == plaintextMessage {
			t.Errorf("Unexpected: API returned plaintext message when encryption was enabled")
		}

		// Check Notifier Encryption
		returnedNotifier := response.Notifiers[0]
		if returnedNotifier == plaintextNotifier {
			t.Errorf("Unexpected: API returned plaintext notifier when encryption was enabled")
		}

		if strings.Contains(returnedNotifier, "://") {
			t.Errorf("Expected encrypted string, but found a URL scheme in: %s", returnedNotifier)
		}
	})
}

func TestGetHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Seed data: 1 unsent, 2 sent
	_, err := store.Create(api.Switch{Message: "m1", Notifiers: []string{"active-1"}, CheckInInterval: "1h"})
	if err != nil {
		t.Fatalf("failed to create switch: %v", err)
	}
	s2, err := store.Create(api.Switch{Message: "m2", Notifiers: []string{"triggered-1"}, CheckInInterval: "1h"})
	if err != nil {
		t.Fatalf("failed to create switch: %v", err)
	}
	s3, err := store.Create(api.Switch{Message: "m3", Notifiers: []string{"triggered-2"}, CheckInInterval: "1h"})
	if err != nil {
		t.Fatalf("failed to create switch: %v", err)
	}

	// Mark two as sent
	_ = store.Sent(*s2.Id)
	_ = store.Sent(*s3.Id)

	t.Run("returns all switches with messages", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/switch", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		err := json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode switch: %v", err)
		}

		if len(resp) != 3 {
			t.Errorf("expected 3 switches, got %d", len(resp))
		}

		if resp[0].Message == "" {
			t.Error("expected message field to be populated in GET response")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/switch?limit=1", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		err := json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to unmarshal switches: %v", err)
		}

		if len(resp) != 1 {
			t.Errorf("expected 1 switch, got %d", len(resp))
		}
	})
}

func TestGetByIDHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Seed a switch
	created, err := store.Create(api.Switch{
		Message:         "Find Me",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
	})
	if err != nil {
		t.Fatalf("failed to seed switch: %v", err)
	}

	t.Run("successfully retrieves a switch by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/switch/1", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Get("/api/v1/switch/{id}", s.GetByIDHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if *resp.Id != *created.Id {
			t.Errorf("expected ID %d, got %d", *created.Id, *resp.Id)
		}

		if resp.Message != created.Message {
			t.Errorf("expected message %s, got %s", created.Message, resp.Message)
		}
	})

	t.Run("returns 404 for non-existent ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/switch/999", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Get("/api/v1/switch/{id}", s.GetByIDHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

func TestPutSwitchId(t *testing.T) {
	s, store := setupTestHandler(t)
	v := validator.New()
	mw := middleware.SwitchValidator(v)

	// Create initial switch with deleteAfterSent: false
	initial := api.Switch{
		Message:         "Original Message",
		Notifiers:       []string{"generic://general1"},
		CheckInInterval: "24h",
		DeleteAfterSent: false,
	}
	created, err := store.Create(initial)
	if err != nil {
		t.Fatalf("failed to seed switch: %v", err)
	}

	t.Run("successfully updates an existing switch including deleteAfterSent", func(t *testing.T) {
		// Toggle deleteAfterSent to true and change other fields
		updatedPayload := api.Switch{
			Message:         "Updated Message",
			Notifiers:       []string{"generic://general2"},
			CheckInInterval: "12h",
			DeleteAfterSent: true,
		}
		body, err := json.Marshal(updatedPayload)
		if err != nil {
			t.Fatalf("failed to unmarshal updated switch: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/switch/%d", *created.Id), bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.With(mw).Put("/api/v1/switch/{id}", s.PutByIDHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify all fields updated correctly
		if resp.Message != updatedPayload.Message {
			t.Errorf("expected message %s, got %s", updatedPayload.Message, resp.Message)
		}
		if resp.DeleteAfterSent != updatedPayload.DeleteAfterSent {
			t.Errorf("expected deleteAfterSent %v, got %v", updatedPayload.DeleteAfterSent, resp.DeleteAfterSent)
		}
		if resp.CheckInInterval != updatedPayload.CheckInInterval {
			t.Errorf("expected interval %s, got %s", updatedPayload.CheckInInterval, resp.CheckInInterval)
		}
	})

	t.Run("returns 404 for non-existent switch", func(t *testing.T) {
		body, _ := json.Marshal(initial)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/switch/999", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.With(mw).Put("/api/v1/switch/{id}", s.PutByIDHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

func TestDeleteHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Create a switch to delete
	created, err := store.Create(api.Switch{
		Message:         "Delete Me",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
	})
	if err != nil {
		t.Fatalf("failed to seed switch: %v", err)
	}

	t.Run("successfully deletes an existing switch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/switch/1", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Delete("/api/v1/switch/{id}", s.DeleteHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		// Verify it's gone from the DB
		_, err = store.GetByID(*created.Id)
		if err != sql.ErrNoRows {
			t.Errorf("expected ErrNoRows after delete, got %v", err)
		}
	})

	t.Run("returns 404 for deleting non-existent switch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/switch/999", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Delete("/api/v1/switch/{id}", s.DeleteHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

func TestResetHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Create a switch that was already "sent" to test if reset clears it
	sw, err := store.Create(api.Switch{
		Message:         "Reset Me",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
	})
	if err != nil {
		t.Fatalf("failed to seed switch: %v", err)
	}

	err = store.Sent(*sw.Id)
	if err != nil {
		t.Fatalf("failed to mark switch as sent: %v", err)
	}

	t.Run("successfully resets a switch timer and sent status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch/1/reset", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/api/v1/switch/{id}/reset", s.ResetHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify sent status is now false
		if resp.Sent != nil && *resp.Sent {
			t.Error("expected switch to be unsent after reset")
		}

		// Verify sendAt is in the future
		if resp.SendAt != nil && resp.SendAt.Before(time.Now()) {
			t.Error("expected sendAt to be reset to a future time")
		}
	})

	t.Run("returns 404 for resetting non-existent switch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch/999/reset", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/api/v1/switch/{id}/reset", s.ResetHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("reset enables a disabled switch", func(t *testing.T) {
		disabledSw, err := store.Create(api.Switch{
			Message:         "Reset Me",
			Notifiers:       []string{"logger://"},
			CheckInInterval: "1h",
			Disabled:        true,
		})
		if err != nil {
			t.Fatalf("failed to seed switch: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/switch/%d/reset", *disabledSw.Id), nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/api/v1/switch/{id}/reset", s.ResetHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify not disabled
		if resp.Disabled {
			t.Error("expected disabled to be false after reset")
		}
	})
}

func TestDisableHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Create a switch to disable
	sw, err := store.Create(api.Switch{
		Message:         "Disable Me",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
	})
	if err != nil {
		t.Fatalf("failed to create switch: %v", err)
	}

	t.Run("successfully disables a switch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/switch/%d/disable", *sw.Id), nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/api/v1/switch/{id}/disable", s.DisableHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		resp := api.Switch{}
		err = json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify disabled status is now true
		if !resp.Disabled {
			t.Error("expected switch to be disabled in response")
		}

		// Double check the database state
		dbSwitch, err := store.GetByID(*sw.Id)
		if err != nil {
			t.Fatalf("failed to get switch by id: %v", err)
		}

		if !dbSwitch.Disabled {
			t.Error("expected switch to be disabled in database")
		}
	})

	t.Run("returns 404 for disabling non-existent switch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/switch/999/disable", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/api/v1/switch/{id}/disable", s.DisableHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

func TestSwitch_Redact(t *testing.T) {
	s := &Switch{}

	t.Run("Replaces populated subscription with empty object", func(t *testing.T) {
		endpoint := "https://fcm.googleapis.com/test"
		sw := api.Switch{
			PushSubscription: &api.PushSubscription{
				Endpoint: &endpoint,
			},
		}

		redacted := s.redact(sw)

		// Verify object exists (signals UI to show the bell)
		if redacted.PushSubscription == nil {
			t.Fatal("expected PushSubscription to be non-nil")
		}

		// Verify all internal fields are nil (redacted)
		if redacted.PushSubscription.Endpoint != nil {
			t.Error("expected Endpoint to be nil")
		}

		if redacted.PushSubscription.Keys != nil {
			t.Error("expected Keys to be nil")
		}
	})

	t.Run("Stays nil if original was nil", func(t *testing.T) {
		sw := api.Switch{PushSubscription: nil}
		redacted := s.redact(sw)
		if redacted.PushSubscription != nil {
			t.Error("expected PushSubscription to remain nil")
		}
	})
}
func TestSwitch_RedactAll(t *testing.T) {
	s := &Switch{}

	t.Run("Correctly redacts a mixture of set and unset subscriptions", func(t *testing.T) {
		endpoint := "https://example.com"
		switches := []api.Switch{
			{Id: ptr(1), PushSubscription: &api.PushSubscription{Endpoint: &endpoint}}, // Has sub
			{Id: ptr(2), PushSubscription: nil},                                        // No sub
		}

		redactedList := s.redactAll(switches)

		if len(redactedList) != 2 {
			t.Fatalf("expected 2 switches, got %d", len(redactedList))
		}

		// First switch: should have an empty object (not nil, but no endpoint)
		if redactedList[0].PushSubscription == nil {
			t.Error("first switch: expected PushSubscription object to exist for UI signaling")
		} else if redactedList[0].PushSubscription.Endpoint != nil {
			t.Error("first switch: expected endpoint to be nil (redacted)")
		}

		// Second switch: should stay nil
		if redactedList[1].PushSubscription != nil {
			t.Error("second switch: expected PushSubscription to remain nil")
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
