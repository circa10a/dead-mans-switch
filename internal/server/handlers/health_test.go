package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/database"
)

func TestHealthHandleFunc(t *testing.T) {
	store, err := database.NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	defer func() { _ = store.Close() }()

	h := &Health{
		Store: store,
	}

	t.Run("returns ok when database is healthy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		h.GetHandleFunc(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		resp := api.Health{}
		err := json.NewDecoder(rec.Body).Decode(&resp)
		if err != nil {
			t.Fatal(err)
		}

		if resp.Status != api.HealthStatusOk {
			t.Errorf("expected status %s, got %s", api.HealthStatusOk, resp.Status)
		}
	})

	t.Run("returns failed when database is closed", func(t *testing.T) {
		err = store.Close()
		if err != nil {
			t.Fatalf("failed to close test db: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		h.GetHandleFunc(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rec.Code)
		}

		resp := api.Health{}
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Status != api.HealthStatusFailed {
			t.Errorf("expected status %s, got %s", api.HealthStatusFailed, resp.Status)
		}
	})
}
