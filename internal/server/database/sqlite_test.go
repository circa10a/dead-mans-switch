package database

import (
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
)

func setupTestStore(t *testing.T) Store {
	t.Helper()
	dbPath := t.TempDir()
	store, err := New(dbPath, false)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	err = store.Init()
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	return store
}

func TestSQLiteStore_CRUD(t *testing.T) {
	store := setupTestStore(t)
	sw := api.Switch{
		Message:         "Test Message",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
		DeleteAfterSent: false,
	}

	t.Run("Create and GetByID", func(t *testing.T) {
		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}
		if created.Message != sw.Message {
			t.Errorf("expected message %s, got %s", sw.Message, created.Message)
		}

		found, err := store.GetByID(*created.Id)
		if err != nil {
			t.Fatalf("failed to get switch: %v", err)
		}
		if *found.Id != *created.Id {
			t.Errorf("expected id %d, got %d", *created.Id, *found.Id)
		}
	})

	t.Run("Update", func(t *testing.T) {
		created, _ := store.Create(sw)
		updateData := created
		updateData.Message = "Updated"
		updateData.CheckInInterval = "2h"

		updated, err := store.Update(*created.Id, updateData)
		if err != nil {
			t.Fatalf("failed to update: %v", err)
		}
		if updated.Message != "Updated" {
			t.Error("message did not update")
		}
		if updated.CheckInInterval != "2h" {
			t.Error("interval did not update")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		created, _ := store.Create(sw)
		err := store.Delete(*created.Id)
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}

		_, err = store.GetByID(*created.Id)
		if err == nil {
			t.Error("expected error getting deleted switch, got nil")
		}
	})
}

func TestSQLiteStore_StatusAndExpirations(t *testing.T) {
	store := setupTestStore(t)

	t.Run("GetAllBySent and Sent status", func(t *testing.T) {
		sw := api.Switch{Message: "m1", Notifiers: []string{"n1"}, CheckInInterval: "1h"}
		created, _ := store.Create(sw)

		// Verify initially not sent
		list, _ := store.GetAllBySent(false, 10)
		if len(list) != 1 {
			t.Errorf("expected 1 unsent switch, got %d", len(list))
		}

		err := store.Sent(*created.Id)
		if err != nil {
			t.Fatalf("failed to mark sent: %v", err)
		}

		list, _ = store.GetAllBySent(true, 10)
		if len(list) != 1 {
			t.Error("switch not found in sent list")
		}
	})

	t.Run("GetExpired", func(t *testing.T) {
		// Create a switch with a very short interval
		sw := api.Switch{Message: "expired", Notifiers: []string{"n1"}, CheckInInterval: "1ms"}
		_, _ = store.Create(sw)

		// Wait for it to expire
		time.Sleep(2 * time.Millisecond)

		expired, err := store.GetExpired(10)
		if err != nil {
			t.Fatalf("failed to get expired: %v", err)
		}
		if len(expired) == 0 {
			t.Error("expected at least 1 expired switch")
		}
	})
}

func TestSQLiteStore_Reset(t *testing.T) {
	store := setupTestStore(t)
	sw := api.Switch{Message: "m1", Notifiers: []string{"n1"}, CheckInInterval: "24h"}
	created, _ := store.Create(sw)

	// Manually mark sent
	_ = store.Sent(*created.Id)

	err := store.Reset(*created.Id)
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	updated, _ := store.GetByID(*created.Id)
	if *updated.Sent {
		t.Error("expected switch to be unsent after reset")
	}
}

func TestSQLiteStore_Encryption(t *testing.T) {
	dbPath := t.TempDir()
	// New store with encryption enabled
	s, err := New(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	sqlite := s.(*SQLiteStore)
	sqlite.EncryptionKey = []byte("32-byte-long-key-for-testing-!!!")
	_ = sqlite.Init()

	t.Run("Data is stored encrypted and retrieved as encrypted", func(t *testing.T) {
		msg := "secret message"
		sw := api.Switch{Message: msg, Notifiers: []string{"n1"}, CheckInInterval: "1h"}

		created, err := sqlite.Create(sw)
		if err != nil {
			t.Fatal(err)
		}

		// Check that data is not stored in plaintext
		if created.Message == msg {
			t.Errorf("expected different strings due to encryption but got '%s' and '%s'", msg, created.Message)
		}
	})
}
