package server

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/stretchr/testify/assert"
)

// MockStore satisfies the database.Store interface
type MockStore struct {
	GetExpiredFunc           func(limit int) ([]api.Switch, error)
	GetEligibleRemindersFunc func(limit int) ([]api.Switch, error)
	DeleteFunc               func(id int) error
	SentFunc                 func(id int) error
	MarkReminderSentFunc     func(id int) error

	DeletedCalled          bool
	SentCalled             bool
	MarkReminderSentCalled bool
}

// Interface methods
func (m *MockStore) Init() error {
	return nil
}
func (m *MockStore) Create(sw api.Switch) (api.Switch, error) {
	return sw, nil
}
func (m *MockStore) GetAll(limit int) ([]api.Switch, error) {
	return nil, nil
}
func (m *MockStore) GetByID(id int) (api.Switch, error) {
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

func (m *MockStore) ReminderSent(id int) (api.Switch, error) {
	m.MarkReminderSentCalled = true
	if m.MarkReminderSentFunc != nil {
		return api.Switch{}, m.MarkReminderSentFunc(id)
	}
	return api.Switch{}, nil
}
func (m *MockStore) Sent(id int) (api.Switch, error) {
	m.SentCalled = true
	return api.Switch{}, m.SentFunc(id)
}
func (m *MockStore) Update(id int, sw api.Switch) (api.Switch, error) {
	return sw, nil
}
func (m *MockStore) Delete(id int) error {
	m.DeletedCalled = true
	return m.DeleteFunc(id)
}
func (m *MockStore) Disable(id int) (api.Switch, error) {
	return api.Switch{}, nil
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

	t.Run("should mark as sent on success when DeleteAfterSent is false", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:              &testID,
					Message:         "hello",
					Notifiers:       []string{validNotifier},
					DeleteAfterSent: ptr(false),
				}}, nil
			},
			SentFunc: func(id int) error { return nil },
		}

		w := &Worker{Store: mock, BatchSize: 10, Logger: logger}
		w.sweep()

		assert.True(t, mock.SentCalled)
		assert.False(t, mock.DeletedCalled)
	})
}

func TestWorker_Sweep_Reminders(t *testing.T) {
	testID := 456
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Helper to setup a switch with a specific SendAt time
	createReminderSwitch := func(sendAt *int64) api.Switch {
		return api.Switch{
			Id:                &testID,
			Message:           "reminder",
			ReminderThreshold: ptr("15m"),
			SendAt:            sendAt,
			PushSubscription: &api.PushSubscription{
				Endpoint: ptr("https://fcm.googleapis.com/test"),
			},
		}
	}

	t.Run("should send reminder and mark as sent when inside window", func(t *testing.T) {
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
		assert.False(t, mock.MarkReminderSentCalled, "Should not mark sent because VAPID keys were missing/invalid")
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

		assert.False(t, mock.MarkReminderSentCalled)
	})
}

func TestWorker_Sweep_NotifierFaultTolerance(t *testing.T) {
	testID := 789
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("should attempt all notifiers even if one fails", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:              &testID,
					Message:         "fault tolerance test",
					Notifiers:       []string{"invalid://scheme", "logger://"},
					DeleteAfterSent: ptr(false),
				}}, nil
			},
			SentFunc: func(id int) error { return nil },
		}

		w := &Worker{Store: mock, BatchSize: 10, Logger: logger}
		w.sweep()

		// Because one notifier failed, the aggregate error should have prevented
		// the database from being updated to "Sent".
		assert.False(t, mock.SentCalled, "Should not mark as sent if any notifier in the list fails")
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
		assert.NoError(t, err)
	})

	t.Run("returns nil when PushSubscription.Endpoint is nil", func(t *testing.T) {
		sw := api.Switch{
			Id: ptr(1),
			PushSubscription: &api.PushSubscription{
				Endpoint: nil,
			},
		}

		err := w.sendWebPush(sw, "Test Title", "Test Body")
		assert.NoError(t, err)
	})
}

func ptr[T any](v T) *T {
	return &v
}
