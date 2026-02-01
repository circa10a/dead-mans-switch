package database

import (
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
)

func setupTestStore(t *testing.T) Store {
	t.Helper()
	dbPath := t.TempDir()
	store, err := New(dbPath)
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
		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

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

	t.Run("Update includes PushSubscription", func(t *testing.T) {
		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		newPush := &api.PushSubscription{
			Endpoint: ptr("https://new-endpoint.com"),
			Keys: &struct {
				Auth   *string `json:"auth,omitempty"`
				P256dh *string `json:"p256dh,omitempty"`
			}{
				Auth:   ptr("auth-secret"),
				P256dh: ptr("public-key"),
			},
		}

		updateData := created
		updateData.PushSubscription = newPush

		updated, err := store.Update(*created.Id, updateData)
		if err != nil {
			t.Fatalf("failed to update: %v", err)
		}

		if updated.PushSubscription == nil || updated.PushSubscription.Endpoint == nil {
			t.Fatal("PushSubscription or Endpoint is nil after update")
		}

		if *updated.PushSubscription.Endpoint != "https://new-endpoint.com" {
			t.Errorf("expected https://new-endpoint.com, got %s", *updated.PushSubscription.Endpoint)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		err = store.Delete(*created.Id)
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}

		_, err = store.GetByID(*created.Id)
		if err == nil {
			t.Error("expected error getting deleted switch, got nil")
		}
	})

	t.Run("Data is stored encrypted and retrieved as encrypted in API view", func(t *testing.T) {
		msg := "secret message"
		sw := api.Switch{Message: msg, Notifiers: []string{"n1"}, CheckInInterval: "1h", Encrypted: true}

		created, err := store.Create(sw)
		if err != nil {
			t.Fatal(err)
		}

		// This SHOULD be ciphertext now
		if created.Message == msg {
			t.Errorf("Security failure: GetByID returned plaintext for an encrypted switch")
		}

		// Verify it is actually the ciphertext we expect by checking for base64-like length/randomness
		if len(created.Message) < len(msg) {
			t.Errorf("Message too short to be ciphertext")
		}
	})
}

func TestSQLiteStore_Reminders(t *testing.T) {
	store := setupTestStore(t)

	t.Run("Create and Retrieve Reminder Fields", func(t *testing.T) {
		sw := api.Switch{
			Message:           "Reminder Test",
			Notifiers:         []string{"logger://"},
			CheckInInterval:   "1h",
			ReminderThreshold: ptr("15m"),
		}

		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create: %v", err)
		}

		if created.ReminderThreshold == nil || *created.ReminderThreshold != "15m" {
			t.Errorf("expected 15m, got %v", created.ReminderThreshold)
		}

		if created.ReminderSent == nil || *created.ReminderSent {
			t.Error("expected reminder_sent to be false initially")
		}
	})

	t.Run("GetEligibleReminders and MarkReminderSent", func(t *testing.T) {
		sw := api.Switch{
			Message:           "Eligible",
			Notifiers:         []string{"logger://"},
			CheckInInterval:   "1h",
			ReminderThreshold: ptr("10m"),
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr("https://example.com"),
			},
		}

		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		eligible, err := store.GetEligibleReminders(10)
		if err != nil {
			t.Fatalf("failed to get eligible: %v", err)
		}

		found := false
		for _, s := range eligible {
			if *s.Id == *created.Id {
				found = true
				break
			}
		}
		if !found {
			t.Error("created switch was not found in eligible reminders list")
		}

		err = store.ReminderSent(*created.Id)
		if err != nil {
			t.Fatalf("failed to mark reminder sent: %v", err)
		}

		eligible, err = store.GetEligibleReminders(10)
		if err != nil {
			t.Fatalf("failed to get eligible reminders: %v", err)
		}
		for _, s := range eligible {
			if *s.Id == *created.Id {
				t.Error("switch still showing as eligible after being marked sent")
			}
		}
	})
}

func TestSQLiteStore_StatusAndExpirations(t *testing.T) {
	store := setupTestStore(t)

	t.Run("GetExpired decrypts all fields for worker", func(t *testing.T) {
		plaintextMsg := "sensitive data"
		notifierURL := "discord://webhook-url"
		pushEndpoint := "https://fcm.googleapis.com/test"

		sw := api.Switch{
			Message:         plaintextMsg,
			Notifiers:       []string{notifierURL},
			CheckInInterval: "1ms",
			Encrypted:       true,
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr(pushEndpoint),
			},
		}

		created, err := store.Create(sw)
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(5 * time.Millisecond)

		expiredList, err := store.GetExpired(10)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, s := range expiredList {
			if *s.Id == *created.Id {
				found = true
				if s.Message != plaintextMsg {
					t.Errorf("Worker expected plaintext message, got: %s", s.Message)
				}
				if len(s.Notifiers) == 0 || s.Notifiers[0] != notifierURL {
					t.Errorf("Worker expected plaintext notifier, got: %v", s.Notifiers)
				}
				if s.PushSubscription == nil || *s.PushSubscription.Endpoint != pushEndpoint {
					t.Errorf("Worker expected decrypted push endpoint, got: %v", s.PushSubscription.Endpoint)
				}
			}
		}
		if !found {
			t.Error("switch not found in expired list")
		}
	})
}

func TestSQLiteStore_Reset(t *testing.T) {
	store := setupTestStore(t)
	sw := api.Switch{Message: "m1", Notifiers: []string{"n1"}, CheckInInterval: "24h"}
	created, err := store.Create(sw)
	if err != nil {
		t.Fatalf("failed to create switch: %v", err)
	}

	_ = store.Sent(*created.Id)
	err = store.Reset(*created.Id)
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	updated, err := store.GetByID(*created.Id)
	if err != nil {
		t.Fatalf("failed to get switch by id: %v", err)
	}

	if *updated.Sent {
		t.Error("expected switch to be unsent after reset")
	}
}

func TestSQLiteStore_StorageHelpers(t *testing.T) {
	store := setupTestStore(t).(*SQLiteStore) // Cast to access internal methods

	// Sample data
	plaintextMsg := "secure message"
	notifiers := []string{"https://webhook.site/123"}
	push := &api.PushSubscription{
		Endpoint: ptr("https://push.com/target"),
	}

	t.Run("prepareSwitchForStorage - Encrypted", func(t *testing.T) {
		sw := api.Switch{
			Message:          plaintextMsg,
			Notifiers:        notifiers,
			PushSubscription: push,
			Encrypted:        true,
		}

		msgOut, notifiersOut, pushOut, err := store.prepareSwitchForStorage(sw)
		if err != nil {
			t.Fatalf("failed to prepare storage: %v", err)
		}

		// Verify encryption happened
		if msgOut == plaintextMsg {
			t.Error("expected message to be encrypted, but it was plaintext")
		}

		if notifiersOut == `["https://webhook.site/123"]` {
			t.Error("expected notifiers to be encrypted JSON, but it was raw JSON")
		}

		if !pushOut.Valid || pushOut.String == "" {
			t.Fatal("expected push subscription to be valid in storage")
		}

		if pushOut.String == `{"endpoint":"https://push.com/target"}` {
			t.Error("expected push sub to be encrypted, but it was raw JSON")
		}
	})

	t.Run("decryptInPlace - End to End", func(t *testing.T) {
		// Prepare data (Encrypted)
		original := api.Switch{
			Message:          plaintextMsg,
			Notifiers:        notifiers,
			PushSubscription: push,
			Encrypted:        true,
		}

		msgEnc, notifiersEnc, pushEnc, err := store.prepareSwitchForStorage(original)
		if err != nil {
			t.Fatalf("prepareSwitchForStorage failed: %v", err)
		}

		// Simulate what scanSwitches does for an encrypted record:
		// It puts the raw DB strings into the struct fields.
		swFromDB := api.Switch{
			Encrypted: true,
			Message:   msgEnc,
			Notifiers: []string{notifiersEnc},
			PushSubscription: &api.PushSubscription{
				Endpoint: &pushEnc.String,
			},
		}

		//  Decrypt
		err = store.decryptInPlace(&swFromDB)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		// Verify equality with original
		if swFromDB.Message != original.Message {
			t.Errorf("expected msg %s, got %s", original.Message, swFromDB.Message)
		}

		if len(swFromDB.Notifiers) != 1 || swFromDB.Notifiers[0] != original.Notifiers[0] {
			t.Errorf("expected notifiers %v, got %v", original.Notifiers, swFromDB.Notifiers)
		}

		if swFromDB.PushSubscription == nil || *swFromDB.PushSubscription.Endpoint != *original.PushSubscription.Endpoint {
			t.Errorf("expected push endpoint %s, got %v", *original.PushSubscription.Endpoint, swFromDB.PushSubscription.Endpoint)
		}
	})

	t.Run("decryptInPlace - Non Encrypted", func(t *testing.T) {
		sw := api.Switch{
			Message:   "plain",
			Encrypted: false,
		}

		// Should do nothing and return no error
		err := store.decryptInPlace(&sw)
		if err != nil {
			t.Errorf("expected no error for non-encrypted switch, got %v", err)
		}
		if sw.Message != "plain" {
			t.Error("message should remain unchanged")
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
