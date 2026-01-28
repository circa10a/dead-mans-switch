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
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// setupTestHandler initializes a Switch handler with a temporary database
func setupTestHandler(t *testing.T) (*Switch, database.Store) {
	t.Helper()

	store, err := database.New(t.TempDir(), false)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	if err := store.Init(); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	s := &Switch{
		Validator: validator.New(),
		Store:     store,
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}

	return s, store
}

func TestPostHandleFunc(t *testing.T) {
	s, _ := setupTestHandler(t)

	t.Run("successfully creates a switch with message", func(t *testing.T) {
		payload := api.Switch{
			Message:         "Secret Message",
			Notifiers:       []string{"logger://", "discord://token@id"},
			CheckInInterval: "24h",
			DeleteAfterSent: true,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		s.PostHandleFunc(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		resp := api.Switch{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

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
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		s.PostHandleFunc(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty message, got %d", rec.Code)
		}
	})
}

func TestPostHandleFunc_Encryption(t *testing.T) {
	tmpDir := t.TempDir()
	// encryption enabled
	store, err := database.New(tmpDir, true)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store.Init(); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	h := &Switch{
		Validator: validator.New(),
		Store:     store,
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}

	t.Run("returns encrypted message and notifiers in response", func(t *testing.T) {
		plaintextNotifier := "discord://webhook-url"
		plaintextMessage := "Top Secret Message"

		payload := api.Switch{
			Message:         plaintextMessage,
			Notifiers:       []string{plaintextNotifier},
			CheckInInterval: "24h",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		h.PostHandleFunc(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		response := api.Switch{}

		err := json.Unmarshal(rec.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Check Message Encryption in Response
		if response.Message == plaintextMessage {
			t.Errorf("Unexpected: API returned plaintext message when encryption was enabled")
		}

		// Check Notifier Encryption in Response
		returnedNotifier := response.Notifiers[0]
		if returnedNotifier == plaintextNotifier {
			t.Errorf("Unexpected: API returned plaintext notifier when encryption was enabled")
		}

		if strings.Contains(returnedNotifier, "://") {
			t.Errorf("Expected encrypted string, but found a URL scheme in: %s", returnedNotifier)
		}

		t.Logf("Encrypted Msg in Response: %s", response.Message)
		t.Logf("Encrypted Notifier in Response: %s", returnedNotifier)
	})
}

func TestGetHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Seed data: 1 unsent, 2 sent
	_, _ = store.Create(api.Switch{Message: "m1", Notifiers: []string{"active-1"}, CheckInInterval: "1h"})
	s2, _ := store.Create(api.Switch{Message: "m2", Notifiers: []string{"triggered-1"}, CheckInInterval: "1h"})
	s3, _ := store.Create(api.Switch{Message: "m3", Notifiers: []string{"triggered-2"}, CheckInInterval: "1h"})

	// Mark two as sent
	_ = store.Sent(*s2.Id)
	_ = store.Sent(*s3.Id)

	t.Run("returns all switches with messages", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/switches", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if len(resp) != 3 {
			t.Errorf("expected 3 switches, got %d", len(resp))
		}

		if resp[0].Message == "" {
			t.Error("expected message field to be populated in GET response")
		}
	})

	t.Run("filters only sent switches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/switches?sent=true", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if len(resp) != 2 {
			t.Errorf("expected 2 sent, got %d", len(resp))
		}

		for _, sw := range resp {
			if sw.Sent != nil && !*sw.Sent {
				t.Errorf("expected switch %d to be sent", *sw.Id)
			}
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/switches?limit=1", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		_ = json.NewDecoder(rec.Body).Decode(&resp)

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
		req := httptest.NewRequest(http.MethodGet, "/switch/1", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Get("/switch/{id}", s.GetByIDHandleFunc)
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
	})

	t.Run("returns 404 for non-existent ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/switch/999", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Get("/switch/{id}", s.GetByIDHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

func TestPutSwitchId(t *testing.T) {
	s, store := setupTestHandler(t)
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
		body, _ := json.Marshal(updatedPayload)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/switch/%d", *created.Id), bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Put("/switch/{id}", s.PutByIDHandleFunc)
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
		req := httptest.NewRequest(http.MethodPut, "/switch/999", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Put("/switch/{id}", s.PutByIDHandleFunc)
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
		req := httptest.NewRequest(http.MethodDelete, "/switch/1", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Delete("/switch/{id}", s.DeleteHandleFunc)
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
		req := httptest.NewRequest(http.MethodDelete, "/switch/999", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Delete("/switch/{id}", s.DeleteHandleFunc)
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
		req := httptest.NewRequest(http.MethodPost, "/switch/1/reset", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/switch/{id}/reset", s.ResetHandleFunc)
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
		req := httptest.NewRequest(http.MethodPost, "/switch/999/reset", nil)
		rec := httptest.NewRecorder()

		r := chi.NewRouter()
		r.Post("/switch/{id}/reset", s.ResetHandleFunc)
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}
