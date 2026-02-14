package database

import (
	"bytes"
	"database/sql"
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
)

var statusActive = api.SwitchStatusActive

func setupTestStore(t *testing.T) Store {
	t.Helper()
	dbPath := t.TempDir()
	store, err := NewSQLiteStore(dbPath)
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
		Message:              "Test Message",
		Notifiers:            []string{"logger://"},
		CheckInInterval:      "1h",
		DeleteAfterTriggered: ptr(false),
		TriggerAt:            &oneHourLater,
		Status:               &statusActive,
	}

	t.Run("Create and GetByID", func(t *testing.T) {
		created, err := store.Create(sw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}
		if created.Message != sw.Message {
			t.Errorf("expected message %s, got %s", sw.Message, created.Message)
		}

		found, err := store.GetByID("admin", *created.Id)
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
			Encrypted:       ptr(true),
			TriggerAt:       &oneHourLater,
			Status:          &statusActive,
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
			ReminderEnabled:   ptr(true),
			ReminderThreshold: ptr("15m"),
			TriggerAt:         &oneHourLater,
			Status:            &statusActive,
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

	t.Run("GetEligibleReminders", func(t *testing.T) {
		oneSecondAgo := time.Now().Unix() - 1
		sw := api.Switch{
			Message:           "Eligible",
			Notifiers:         []string{"logger://"},
			CheckInInterval:   "1h",
			ReminderEnabled:   ptr(true),
			ReminderThreshold: ptr("10m"),
			TriggerAt:         &oneSecondAgo, // Expired
			Status:            &statusActive,
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

		created.ReminderSent = ptr(true)
		_, err = store.Update(*created.Id, created)
		if err != nil {
			t.Fatalf("failed to mark reminder sent: %v", err)
		}

		eligible, err = store.GetEligibleReminders(10)
		if err != nil {
			t.Fatalf("failed to get eligible reminders: %v", err)
		}
		for _, s := range eligible {
			if *s.Id == *created.Id {
				t.Error("switch still showing reminder as eligible after being marked sent")
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
			Encrypted:       ptr(true),
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr(pushEndpoint),
			},
			TriggerAt: &tenSecondsAgo,
			Status:    &statusActive,
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
			Encrypted:        ptr(true),
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

func TestSQLiteStore_UserScoping(t *testing.T) {
	store := setupTestStore(t)
	oneHourLater := time.Now().Add(time.Hour).Unix()

	user1 := "user1@example.com"
	user2 := "user2@example.com"

	sw1 := api.Switch{
		Message:         "User1 Switch",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
		TriggerAt:       &oneHourLater,
		Status:          &statusActive,
		UserId:          &user1,
	}

	sw2 := api.Switch{
		Message:         "User2 Switch",
		Notifiers:       []string{"logger://"},
		CheckInInterval: "1h",
		TriggerAt:       &oneHourLater,
		Status:          &statusActive,
		UserId:          &user2,
	}

	t.Run("Create assigns userId", func(t *testing.T) {
		created, err := store.Create(sw1)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}
		if created.UserId == nil || *created.UserId != user1 {
			t.Errorf("expected userId %s, got %v", user1, created.UserId)
		}
	})

	t.Run("Create defaults to admin when userId is nil", func(t *testing.T) {
		noUserSw := api.Switch{
			Message:         "No User Switch",
			Notifiers:       []string{"logger://"},
			CheckInInterval: "1h",
			TriggerAt:       &oneHourLater,
			Status:          &statusActive,
		}
		created, err := store.Create(noUserSw)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}
		if created.UserId == nil || *created.UserId != "admin" {
			t.Errorf("expected userId 'admin', got %v", created.UserId)
		}
	})

	t.Run("GetAll only returns switches for the specified user", func(t *testing.T) {
		_, err := store.Create(sw2)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		user1Switches, err := store.GetAll(user1, -1)
		if err != nil {
			t.Fatalf("failed to get switches: %v", err)
		}
		for _, s := range user1Switches {
			if s.UserId == nil || *s.UserId != user1 {
				t.Errorf("expected all switches to belong to %s, got %v", user1, s.UserId)
			}
		}

		user2Switches, err := store.GetAll(user2, -1)
		if err != nil {
			t.Fatalf("failed to get switches: %v", err)
		}
		for _, s := range user2Switches {
			if s.UserId == nil || *s.UserId != user2 {
				t.Errorf("expected all switches to belong to %s, got %v", user2, s.UserId)
			}
		}

		if len(user1Switches) == 0 {
			t.Error("expected user1 to have switches")
		}
		if len(user2Switches) == 0 {
			t.Error("expected user2 to have switches")
		}
	})

	t.Run("GetByID returns not found for wrong user", func(t *testing.T) {
		created, err := store.Create(sw1)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		// user2 should not see user1's switch
		_, err = store.GetByID(user2, *created.Id)
		if err != sql.ErrNoRows {
			t.Errorf("expected ErrNoRows for wrong user, got %v", err)
		}

		// user1 should see their own switch
		found, err := store.GetByID(user1, *created.Id)
		if err != nil {
			t.Fatalf("expected to find switch for correct user: %v", err)
		}
		if *found.Id != *created.Id {
			t.Errorf("expected id %d, got %d", *created.Id, *found.Id)
		}
	})

	t.Run("Update fails for wrong user", func(t *testing.T) {
		created, err := store.Create(sw1)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		updateData := created
		updateData.Message = "Hacked"
		updateData.UserId = &user2

		_, err = store.Update(*created.Id, updateData)
		if err != sql.ErrNoRows {
			t.Errorf("expected ErrNoRows when updating as wrong user, got %v", err)
		}
	})

	t.Run("Delete fails for wrong user", func(t *testing.T) {
		created, err := store.Create(sw1)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		// user2 tries to delete user1's switch - should not delete
		err = store.Delete(user2, *created.Id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// switch should still exist for user1
		found, err := store.GetByID(user1, *created.Id)
		if err != nil {
			t.Fatalf("switch should still exist after wrong-user delete: %v", err)
		}
		if *found.Id != *created.Id {
			t.Error("switch was unexpectedly deleted by wrong user")
		}
	})

	t.Run("Delete succeeds for correct user", func(t *testing.T) {
		created, err := store.Create(sw1)
		if err != nil {
			t.Fatalf("failed to create switch: %v", err)
		}

		err = store.Delete(user1, *created.Id)
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}

		_, err = store.GetByID(user1, *created.Id)
		if err != sql.ErrNoRows {
			t.Errorf("expected ErrNoRows after delete, got %v", err)
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
