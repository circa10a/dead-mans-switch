package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
	"github.com/go-playground/validator/v10"
)

// setupTestHandler initializes a Switch handler with a temporary database
func setupTestHandler(t *testing.T) (*Switch, *database.Store) {
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

	t.Run("successfully creates a switch", func(t *testing.T) {
		payload := api.Switch{
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

		if !reflect.DeepEqual(resp.Notifiers, payload.Notifiers) {
			t.Errorf("expected notifier %v, got %v", payload.Notifiers, resp.Notifiers)
		}
	})

	t.Run("returns 400 for invalid validation", func(t *testing.T) {
		payload := api.Switch{
			Notifiers: []string{}, // fails min=1 validation
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/switch", bytes.NewBuffer(body))
		rec := httptest.NewRecorder()

		s.PostHandleFunc(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
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

	t.Run("returns encrypted notifiers in response", func(t *testing.T) {
		plaintextNotifier := "discord://webhook-url"
		payload := `{"notifiers": ["` + plaintextNotifier + `"], "checkInInterval": "24h"}`

		req := httptest.NewRequest(http.MethodPost, "/switch", strings.NewReader(payload))
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

		// The notifier in the response should NOT be the plaintext URL
		if len(response.Notifiers) == 0 {
			t.Fatal("response notifiers slice is empty")
		}

		returnedValue := response.Notifiers[0]
		if returnedValue == plaintextNotifier {
			t.Errorf("Unexpected: API returned plaintext notifier when encryption was enabled")
		}

		// Simple check to see if it's encrypted (not a URL scheme)
		if strings.Contains(returnedValue, "://") {
			t.Errorf("Expected encrypted string, but found a URL scheme in: %s", returnedValue)
		}

		t.Logf("Plaintext: %s", plaintextNotifier)
		t.Logf("Encrypted in Response: %s", returnedValue)
	})
}

func TestGetHandleFunc(t *testing.T) {
	s, store := setupTestHandler(t)

	// Seed data: 1 unsent, 2 sent
	_, _ = store.Create(api.Switch{Notifiers: []string{"active-1"}, CheckInInterval: "1h"})
	s2, _ := store.Create(api.Switch{Notifiers: []string{"triggered-1"}, CheckInInterval: "1h"})
	s3, _ := store.Create(api.Switch{Notifiers: []string{"triggered-2"}, CheckInInterval: "1h"})

	// Mark two as sent
	_ = store.Sent(*s2.Id)
	_ = store.Sent(*s3.Id)

	t.Run("returns all switches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/switches", nil)
		rec := httptest.NewRecorder()
		s.GetHandleFunc(rec, req)

		var resp []api.Switch
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if len(resp) != 3 {
			t.Errorf("expected 3 switches, got %d", len(resp))
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
