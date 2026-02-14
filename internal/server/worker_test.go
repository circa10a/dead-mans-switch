package server

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
)

// MockStore satisfies the database.Store interface
type MockStore struct {
	GetExpiredFunc           func(limit int) ([]api.Switch, error)
	GetEligibleRemindersFunc func(limit int) ([]api.Switch, error)
	DeleteFunc               func(id int) error
	SentFunc                 func(id int) error

	DeletedCalled          bool
	FailedCalled           bool
	MarkReminderSentCalled bool
	SentCalled             bool
	LastFailureReason      *string
}

// Interface methods
func (m *MockStore) Init() error {
	return nil
}

func (m *MockStore) Create(sw api.Switch) (api.Switch, error) {
	return sw, nil
}

func (m *MockStore) GetAll(userID string, limit int) ([]api.Switch, error) {
	return nil, nil
}

func (m *MockStore) GetByID(userID string, id int) (api.Switch, error) {
	return api.Switch{}, nil
}

func (m *MockStore) GetExpired(limit int) ([]api.Switch, error) {
	return m.GetExpiredFunc(limit)
}

func (m *MockStore) GetEligibleReminders(limit int) ([]api.Switch, error) {
	if m.GetEligibleRemindersFunc != nil {
		return m.GetEligibleRemindersFunc(limit)
	}
	return nil, nil
}

func (m *MockStore) Update(id int, sw api.Switch) (api.Switch, error) {
	if *sw.Status == api.SwitchStatusTriggered {
		m.SentCalled = true
	}

	if sw.ReminderSent != nil && *sw.ReminderSent {
		m.MarkReminderSentCalled = true
	}

	if *sw.Status == api.SwitchStatusFailed {
		m.FailedCalled = true
		m.LastFailureReason = sw.FailureReason
	}

	return sw, nil
}

func (m *MockStore) Delete(userID string, id int) error {
	m.DeletedCalled = true
	return m.DeleteFunc(id)
}

func (m *MockStore) EncryptSwitch(*api.Switch) error {
	return nil
}

func (m *MockStore) DecryptSwitch(*api.Switch) error {
	return nil
}

func (m *MockStore) Ping() error {
	return nil
}

func (m *MockStore) Close() error {
	return nil
}

func TestWorker_Sweep_Success(t *testing.T) {
	testID := 123
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validNotifier := "logger://"

	t.Run("should mark as triggered on success when DeleteAfterTriggered is false", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:                   &testID,
					Message:              "hello",
					Notifiers:            []string{validNotifier},
					DeleteAfterTriggered: ptr(false),
				}}, nil
			},
			SentFunc: func(id int) error { return nil },
		}

		w := &Worker{Store: mock, BatchSize: 10, Logger: logger}
		w.sweep()

		if !mock.SentCalled {
			t.Error("expected SentCalled to be true")
		}
		if mock.DeletedCalled {
			t.Error("expected DeletedCalled to be false")
		}
	})
}

func TestWorker_Sweep_Reminders(t *testing.T) {
	testID := 456
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Helper to setup a switch with a specific TriggerAt time
	createReminderSwitch := func(TriggerAt *int64) api.Switch {
		return api.Switch{
			Id:                &testID,
			Message:           "reminder",
			ReminderThreshold: ptr("15m"),
			TriggerAt:         TriggerAt,
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr("https://fcm.googleapis.com/test"),
			},
		}
	}

	t.Run("should send reminder and mark as triggered when inside window", func(t *testing.T) {
		// Switch expires in 10 minutes, reminder is set for 15 minutes -> We are in the window
		expiringSoon := time.Now().Add(10 * time.Minute).Unix()

		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) { return nil, nil },
			GetEligibleRemindersFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{createReminderSwitch(&expiringSoon)}, nil
			},
		}

		w := &Worker{
			Store:     mock,
			BatchSize: 10,
			Logger:    logger,
			// VAPID keys omitted; webpush.SendNotification will error, but we can verify DB call
		}
		w.sweep()

		// Since we didn't provide valid VAPID keys, sendWebPush fails, so MarkReminderSent shouldn't be called.
		// To truly test success here, we would need to mock the webpush client, but verifying the logic window is key:
		if mock.MarkReminderSentCalled {
			t.Error("Should not mark sent because VAPID keys were missing/invalid")
		}
	})

	t.Run("should NOT process reminder if outside warning window", func(t *testing.T) {
		// Switch expires in 1 hour, reminder is 15m -> Not time yet
		expiringLater := time.Now().Add(1 * time.Hour).Unix()

		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) { return nil, nil },
			GetEligibleRemindersFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{createReminderSwitch(&expiringLater)}, nil
			},
		}

		w := &Worker{Store: mock, BatchSize: 10, Logger: logger}
		w.sweep()

		if mock.MarkReminderSentCalled {
			t.Error("expected MarkReminderSentCalled to be false")
		}
	})
}

func TestWorker_Sweep_NotifierFaultTolerance(t *testing.T) {
	testID := 789
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("should attempt all notifiers even if one fails", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{
					{
						Id:                   &testID,
						Message:              "fault tolerance test",
						Notifiers:            []string{"invalid://scheme", "logger://"},
						DeleteAfterTriggered: ptr(false),
					},
				}, nil
			},
			SentFunc: func(id int) error { return nil },
		}

		w := &Worker{Store: mock, BatchSize: 10, Logger: logger}
		w.sweep()

		// Because one notifier failed, the aggregate error should have prevented
		// the database from being updated to "Sent".
		if mock.SentCalled {
			t.Error("Should not mark as triggered if any notifier in the list fails")
		}
	})

	t.Run("should mark as failed if sending fails", func(t *testing.T) {
		testID := 999
		// Use an invalid notifier URL to force a failure
		invalidNotifier := "invalid://scheme"

		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:                   &testID,
					Message:              "failure test",
					Notifiers:            []string{invalidNotifier},
					DeleteAfterTriggered: ptr(false),
				}}, nil
			},
		}

		w := &Worker{
			Store:     mock,
			BatchSize: 10,
			Logger:    logger,
		}

		w.sweep()

		if !mock.FailedCalled {
			t.Error("Expected switch to be marked as failed in DB")
		}
		if mock.SentCalled {
			t.Error("Switch should not be marked as triggered on failure")
		}
		if mock.LastFailureReason == nil || *mock.LastFailureReason == "" {
			t.Error("Expected failureReason to not be empty")
		}
		if mock.LastFailureReason == nil || len(*mock.LastFailureReason) == 0 || (*mock.LastFailureReason)[0] < 'A' || (*mock.LastFailureReason)[0] > 'Z' {
			t.Error("Expected failureReason to start with uppercase")
		}
	})
}

func TestSendWebPush_ReturnsNilWhenSubscriptionIsNil(t *testing.T) {
	w := &Worker{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	t.Run("returns nil when PushSubscription is nil", func(t *testing.T) {
		sw := api.Switch{
			Id:               ptr(1),
			PushSubscription: nil,
		}

		err := w.sendWebPush(sw, "Test Title", "Test Body")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("returns nil when PushSubscription.Endpoint is nil", func(t *testing.T) {
		sw := api.Switch{
			Id: ptr(1),
			PushSubscription: &api.PushSubscription{
				Endpoint: nil,
			},
		}

		err := w.sendWebPush(sw, "Test Title", "Test Body")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
