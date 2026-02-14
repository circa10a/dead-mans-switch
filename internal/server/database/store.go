package database

import "github.com/circa10a/dead-mans-switch/api"

const (
	secretName = "switches_encryption.key"
)

// Store defines the behaviors required for persisting and managing dead man switches.
type Store interface {
	// Init executes the initial schema setup.
	Init() error
	// Close terminates the database connection.
	Close() error
	// Create persists a new switch and returns the created record. Uses sw.UserId for ownership.
	Create(sw api.Switch) (api.Switch, error)
	// DecryptSwitch decrypts sensitive content.
	DecryptSwitch(*api.Switch) error
	// Delete removes a switch record from the store, scoped to the given user.
	Delete(userID string, id int) error
	// EncryptSwitch encrypts sensitive content.
	EncryptSwitch(*api.Switch) error
	// GetAll retrieves a list of switches up to the specified limit, scoped to the given user.
	GetAll(userID string, limit int) ([]api.Switch, error)
	// GetByID retrieves a single switch by its unique identifier, scoped to the given user.
	GetByID(userID string, id int) (api.Switch, error)
	// GetEligibleReminders retrieves switches that are approaching expiry but haven't had a reminder sent yet.
	GetEligibleReminders(limit int) ([]api.Switch, error)
	// GetExpired retrieves switches whose trigger_at time has passed but haven't been sent.
	GetExpired(limit int) ([]api.Switch, error)
	// Ping verifies the database connection is alive.
	Ping() error
	// Update updates an existing switch. Uses sw.UserId for ownership scoping.
	Update(id int, sw api.Switch) (api.Switch, error)
}
