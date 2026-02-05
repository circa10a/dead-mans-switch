package database

import (
	"bytes"
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
	oneHourLater := time.Now().Add(time.Hour).Unix()
	sw := api.Switch{
		Message:         "Test Message",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
		DeleteAfterSent: false,
		SendAt:          &oneHourLater,
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

	t.Run("Data is stored encrypted and retrieved as encrypted in API view", func(t *testing.T) {
		msg := "secret message"
		oneHourLater := time.Now().Add(time.Hour).Unix()
		sw := api.Switch{
			Message:         msg,
			Notifiers:       []string{"n1"},
			CheckInInterval: "1h",
			Encrypted:       true,
			SendAt:          &oneHourLater,
		}

		// Store.Create internally calls EncryptSwitch
		created, err := store.Create(sw)
		if err != nil {
			t.Fatal(err)
		}

		// GetByID returns the DB state (scanSwitches does not decrypt)
		if created.Message == msg {
			t.Errorf("Security failure: GetByID returned plaintext for an encrypted switch")
		}

		if len(created.Message) < len(msg) {
			t.Errorf("Message too short to be ciphertext")
		}
	})
}

func TestSQLiteStore_Reminders(t *testing.T) {
	store := setupTestStore(t)

	t.Run("Create and Retrieve Reminder Fields", func(t *testing.T) {
		oneHourLater := time.Now().Add(time.Hour).Unix() // In spongebob's voice
		sw := api.Switch{
			Message:           "Reminder Test",
			Notifiers:         []string{"logger://"},
			CheckInInterval:   "1h",
			ReminderThreshold: ptr("15m"),
			SendAt:            &oneHourLater,
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
		oneSecondAgo := time.Now().Unix() - 1
		sw := api.Switch{
			Message:           "Eligible",
			Notifiers:         []string{"logger://"},
			CheckInInterval:   "1h",
			ReminderThreshold: ptr("10m"),
			SendAt:            &oneSecondAgo, // Expired
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

		tenSecondsAgo := time.Now().Unix() - 10
		sw := api.Switch{
			Message:         plaintextMsg,
			Notifiers:       []string{notifierURL},
			CheckInInterval: "1ms",
			Encrypted:       true,
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr(pushEndpoint),
			},
			SendAt: &tenSecondsAgo,
		}

		created, err := store.Create(sw)
		if err != nil {
			t.Fatal(err)
		}

		// GetExpired calls DecryptSwitch internally
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

func TestSQLiteStore_SwitchCryptoHelpers(t *testing.T) {
	store := setupTestStore(t).(*SQLiteStore)

	plaintextMsg := "secure message"
	notifiers := []string{"https://webhook.site/123"}
	push := &api.PushSubscription{
		Endpoint: ptr("https://push.com/target"),
	}

	t.Run("EncryptSwitch / DecryptSwitch Round Trip", func(t *testing.T) {
		sw := api.Switch{
			Message:          plaintextMsg,
			Notifiers:        notifiers,
			PushSubscription: push,
			Encrypted:        true,
		}

		// 1. Encrypt
		err := store.EncryptSwitch(&sw)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		if sw.Message == plaintextMsg {
			t.Error("expected message to be encrypted")
		}

		// 2. Decrypt
		err = store.DecryptSwitch(&sw)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		if sw.Message != plaintextMsg {
			t.Errorf("expected msg %s, got %s", plaintextMsg, sw.Message)
		}

		if sw.Notifiers[0] != notifiers[0] {
			t.Errorf("expected notifier %s, got %s", notifiers[0], sw.Notifiers[0])
		}

		if *sw.PushSubscription.Endpoint != *push.Endpoint {
			t.Errorf("expected push endpoint %s, got %s", *push.Endpoint, *sw.PushSubscription.Endpoint)
		}
	})
}

func TestEncryptionPrimitives(t *testing.T) {
	key := []byte("this-is-a-32-byte-long-test-key!")
	store := &SQLiteStore{EncryptionKey: key}
	plaintext := []byte("Hello, Dead Man's Switch!")

	t.Run("successfully encrypts and decrypts", func(t *testing.T) {
		ciphertext, err := store.encrypt(plaintext) // Calling lowercase internal methods
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		decrypted, err := store.decrypt(ciphertext)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("expected %s, got %s", string(plaintext), string(decrypted))
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
