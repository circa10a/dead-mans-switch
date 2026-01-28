package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/circa10a/dead-mans-switch/api"
)

// MockStore satisfies the database.Store interface
type MockStore struct {
	GetExpiredFunc func(limit int) ([]api.Switch, error)
	DeleteFunc     func(id int) error
	SentFunc       func(id int) error

	DeletedCalled bool
	SentCalled    bool
}

// Interface methods
func (m *MockStore) Init() error                                             { return nil }
func (m *MockStore) Create(sw api.Switch) (api.Switch, error)                { return sw, nil }
func (m *MockStore) GetAll(limit int) ([]api.Switch, error)                  { return nil, nil }
func (m *MockStore) GetAllBySent(sent bool, limit int) ([]api.Switch, error) { return nil, nil }
func (m *MockStore) GetByID(id int) (api.Switch, error)                      { return api.Switch{}, nil }
func (m *MockStore) GetExpired(limit int) ([]api.Switch, error)              { return m.GetExpiredFunc(limit) }
func (m *MockStore) Sent(id int) error {
	m.SentCalled = true
	return m.SentFunc(id)
}
func (m *MockStore) Update(id int, sw api.Switch) (api.Switch, error) { return sw, nil }
func (m *MockStore) Delete(id int) error {
	m.DeletedCalled = true
	return m.DeleteFunc(id)
}
func (m *MockStore) Reset(id int) error { return nil }
func (m *MockStore) Ping() error        { return nil }
func (m *MockStore) Close() error       { return nil }

func TestWorker_Sweep(t *testing.T) {
	testID := 1
	// Create a logger that doesn't spam the test output
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("should NOT mark as sent if notification fails", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:              &testID,
					Message:         "test alert",
					Notifiers:       []string{"invalid://scheme"}, // Will cause error
					DeleteAfterSent: false,
				}}, nil
			},
			SentFunc: func(id int) error { return nil },
		}

		w := &Worker{
			Store:     mock,
			BatchSize: 10,
			Logger:    logger,
		}

		w.sweep()

		if mock.SentCalled {
			t.Error("Sent() was called despite notification failure")
		}
	})

	t.Run("should NOT delete if notification fails", func(t *testing.T) {
		mock := &MockStore{
			GetExpiredFunc: func(limit int) ([]api.Switch, error) {
				return []api.Switch{{
					Id:              &testID,
					Message:         "test alert",
					Notifiers:       []string{"invalid://scheme"},
					DeleteAfterSent: true,
				}}, nil
			},
			DeleteFunc: func(id int) error { return nil },
		}

		w := &Worker{
			Store:     mock,
			BatchSize: 10,
			Logger:    logger,
		}

		w.sweep()

		if mock.DeletedCalled {
			t.Error("Delete() was called despite notification failure")
		}
	})
}
