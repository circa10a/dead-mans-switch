package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/secrets"
	_ "modernc.org/sqlite"
)

const (
	dbName     = "switches.db"
	secretName = "switches_encryption.key"
	// switchColumns centralizes the field list to prevent Scan errors
	switchColumns = `id, notifier, send_at, sent, check_in_interval, delete_after_sent, encrypted`
)

// Store wraps the database connection and provides access methods
type Store struct {
	db            *sql.DB
	EncryptionKey []byte
}

// New initializes a new Store with a path to a directory for the database.
func New(dbPath string, encryptionEnabled bool) (*Store, error) {
	db, err := connect(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	var key []byte

	if encryptionEnabled {
		keyPath := filepath.Join(dbPath, secretName)
		key, err = secrets.LoadOrCreateKey(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize encryption key: %w", err)
		}
	}

	return &Store{db: db, EncryptionKey: key}, nil
}

// Init executes the schema setup for creating the db.
func (s *Store) Init() error {
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// Create creates a new dead man switch.
func (s *Store) Create(sw api.Switch) (api.Switch, error) {
	notifiersJSON, err := json.Marshal(sw.Notifiers)
	if err != nil {
		return api.Switch{}, fmt.Errorf("failed to marshal notifier list: %w", err)
	}

	encrypted := false
	notifiersToStore := string(notifiersJSON)

	if len(s.EncryptionKey) > 0 {
		encryptedNotifiersJSON, err := s.encrypt(notifiersJSON)
		if err != nil {
			return api.Switch{}, fmt.Errorf("failed to encrypt notifiers data: %w", err)
		}
		notifiersToStore = encryptedNotifiersJSON
		encrypted = true
	}

	duration, err := time.ParseDuration(sw.CheckInInterval)
	if err != nil {
		return api.Switch{}, fmt.Errorf("invalid checkInInterval: %w", err)
	}

	initialSendAt := time.Now().UTC().Add(duration)

	query := `INSERT INTO switches (notifier, send_at, check_in_interval, delete_after_sent, encrypted)
              VALUES (?, ?, ?, ?, ?)`

	res, err := s.db.Exec(query,
		string(notifiersToStore),
		initialSendAt.Format(time.RFC3339),
		sw.CheckInInterval,
		sw.DeleteAfterSent,
		encrypted,
	)
	if err != nil {
		return api.Switch{}, fmt.Errorf("db insert failed: %w", err)
	}

	id, _ := res.LastInsertId()
	return s.GetByID(int(id))
}

// GetAll retrieves all switches from the database
func (s *Store) GetAll(limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches LIMIT ?", switchColumns)
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, false)
}

// GetAllBySent retrieves switches filtered by their sent status (true/false).
func (s *Store) GetAllBySent(sent bool, limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE sent = ? LIMIT ?", switchColumns)
	rows, err := s.db.Query(query, sent, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query switches by sent status: %w", err)
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, false)
}

// GetByID retrieves a switch by its ID.
func (s *Store) GetByID(id int) (api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE id = ?", switchColumns)
	rows, err := s.db.Query(query, id)
	if err != nil {
		return api.Switch{}, err
	}
	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows, false)
	if err != nil {
		return api.Switch{}, err
	}

	if len(switches) == 0 {
		return api.Switch{}, sql.ErrNoRows
	}

	return switches[0], nil
}

// GetExpired retrieves switches that need to be sent.
// Returns unencrypted notifiers for sending.
func (s *Store) GetExpired(limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE sent = ? AND send_at <= ? LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, false, time.Now().UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired switches: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, true)
}

// Sent marks a switch as sent.
func (s *Store) Sent(id int) error {
	_, err := s.db.Exec(`UPDATE switches SET sent = ? WHERE id = ?`, true, id)
	return err
}

// Delete removes a switch.
func (s *Store) Delete(id int) error {
	_, err := s.db.Exec(`DELETE FROM switches WHERE id = ?`, id)
	return err
}

// Reset resets the send_at time based on the check_in_interval.
func (s *Store) Reset(id int) error {
	intervalStr := ""

	err := s.db.QueryRow("SELECT check_in_interval FROM switches WHERE id = ?", id).Scan(&intervalStr)
	if err != nil {
		return err
	}

	duration, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	newSendAt := time.Now().UTC().Add(duration)

	updateQuery := `
		UPDATE switches
		SET send_at = ?,
		    sent = 0
		WHERE id = ?`

	result, err := s.db.Exec(updateQuery, newSendAt.Format(time.RFC3339), id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Close closes the underlying database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks the database connection health.
func (s *Store) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database connection lost: %w", err)
	}

	// Run a dummy query to ensure it's actually readable
	result := 0
	err := s.db.QueryRowContext(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		return fmt.Errorf("database not responding to queries: %w", err)
	}

	return nil
}

func (s *Store) scanSwitches(rows *sql.Rows, plaintext bool) ([]api.Switch, error) {
	var switches []api.Switch

	for rows.Next() {
		sw := api.Switch{}
		var notifierRaw string
		var sendAtRaw string
		var encrypted bool

		err := rows.Scan(
			&sw.Id,
			&notifierRaw,
			&sendAtRaw,
			&sw.Sent,
			&sw.CheckInInterval,
			&sw.DeleteAfterSent,
			&encrypted,
		)
		if err != nil {
			return nil, err
		}

		// If it's encrypted and we want plaintext (internal worker) - decrypt then unmarshal
		// If it's encrypted and we don't want plaintext (API responses) - just put raw string in slice
		// If it's not encrypted -> Just Unmarshal normally

		if encrypted {
			if plaintext {
				// Decrypt so the worker can use the real URLs
				decryptedBytes, err := s.decrypt(notifierRaw)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt notifier for id %d: %w", *sw.Id, err)
				}
				if err := json.Unmarshal(decryptedBytes, &sw.Notifiers); err != nil {
					return nil, fmt.Errorf("failed to unmarshal decrypted notifiers: %w", err)
				}
			} else {
				// For API responses, just return the encrypted string
				// We don't unmarshal because notifierRaw it's not JSON, it's a cipher string
				sw.Notifiers = []string{notifierRaw}
			}
		} else {
			// Plaintext in DB, just unmarshal the JSON array
			if err := json.Unmarshal([]byte(notifierRaw), &sw.Notifiers); err != nil {
				return nil, fmt.Errorf("failed to unmarshal plaintext notifiers: %w", err)
			}
		}

		t, err := time.Parse(time.RFC3339, sendAtRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse send_at: %w", err)
		}
		sw.SendAt = &t

		switches = append(switches, sw)
	}

	return switches, nil
}

// connect initializes the connection with WAL mode and Busy Timeout
func connect(dbPath string) (*sql.DB, error) {
	err := os.MkdirAll(dbPath, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	fullPath := filepath.Join(dbPath, dbName)

	params := url.Values{}
	params.Add("_pragma", "journal_mode=WAL")
	params.Add("_pragma", "synchronous=NORMAL")
	params.Add("_busy_timeout", "5000")

	dsn := fmt.Sprintf("%s?%s", fullPath, params.Encode())

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(10) // WAL mode allows multiple readers
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	// Verify connection
	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("failed to reach database: %w", err)
	}

	return db, nil
}
