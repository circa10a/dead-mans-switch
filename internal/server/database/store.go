package database

import "github.com/circa10a/dead-mans-switch/api"

// Store defines the behaviors required for persisting and managing dead man switches.
type Store interface {
	// Init executes the initial schema setup.
	Init() error
	// Create persists a new switch and returns the created record.
	Create(sw api.Switch) (api.Switch, error)
	// GetAll retrieves a list of switches up to the specified limit.
	GetAll(limit int) ([]api.Switch, error)
	// GetAllBySent retrieves switches filtered by their sent status.
	GetAllBySent(sent bool, limit int) ([]api.Switch, error)
	// GetByID retrieves a single switch by its unique identifier.
	GetByID(id int) (api.Switch, error)
	// GetExpired retrieves switches whose send_at time has passed but haven't been sent.
	GetExpired(limit int) ([]api.Switch, error)
	// Sent marks a specific switch as having been processed/sent.
	Sent(id int) error
	// Update modifies an existing switch's message, notifiers, and interval.
	Update(id int, sw api.Switch) (api.Switch, error)
	// Delete removes a switch record from the store.
	Delete(id int) error
	// Reset updates the send_at time for a switch based on its check-in interval.
	Reset(id int) error
	// Ping verifies the database connection is alive.
	Ping() error
	// Close terminates the database connection.
	Close() error
}
