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
	switchColumns = `id, message, notifiers, send_at, sent, check_in_interval, delete_after_sent, encrypted`
)

// SQLiteStore is an implementation of the Store interface for SQLite.
type SQLiteStore struct {
	db                *sql.DB
	EncryptionKey     []byte
	EncryptionEnabled bool
}

// New initializes a new SQLiteStore with optional encryption support.
func New(dbPath string, encryptionEnabled bool) (Store, error) {
	db, err := connect(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	var key []byte
	keyPath := filepath.Join(dbPath, secretName)

	_, key, err = secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize encryption key: %w", err)
	}

	return &SQLiteStore{
		db:                db,
		EncryptionKey:     key,
		EncryptionEnabled: encryptionEnabled,
	}, nil
}

// Init creates the necessary database tables if they do not already exist.
func (s *SQLiteStore) Init() error {
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// Create inserts a new switch into the database, handling encryption if a key is present.
func (s *SQLiteStore) Create(sw api.Switch) (api.Switch, error) {
	notifiersJSON, err := json.Marshal(sw.Notifiers)
	if err != nil {
		return api.Switch{}, fmt.Errorf("failed to marshal notifier list: %w", err)
	}

	encrypted := false
	notifiersToStore := string(notifiersJSON)
	messageToStore := sw.Message

	// Only encrypt if we have a key and encryption is enabled
	// we still need to read previously encrypted data if encryption was disabled later
	if len(s.EncryptionKey) > 0 && s.EncryptionEnabled {
		encNotifiers, err := s.encrypt(notifiersJSON)
		if err != nil {
			return api.Switch{}, fmt.Errorf("failed to encrypt notifiers data: %w", err)
		}

		notifiersToStore = encNotifiers

		if sw.Message != "" {
			encMsg, err := s.encrypt([]byte(sw.Message))
			if err != nil {
				return api.Switch{}, fmt.Errorf("failed to encrypt message: %w", err)
			}

			messageToStore = encMsg
		}

		encrypted = true
	}

	duration, err := time.ParseDuration(sw.CheckInInterval)
	if err != nil {
		return api.Switch{}, fmt.Errorf("invalid checkInInterval: %w", err)
	}

	initialSendAt := time.Now().UTC().Add(duration)

	query := `INSERT INTO switches (message, notifiers, send_at, sent, check_in_interval, delete_after_sent, encrypted)
              VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.Exec(query,
		messageToStore,
		notifiersToStore,
		initialSendAt.Format(time.RFC3339),
		false,
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

// GetAll returns all switch records in the database up to the provided limit.
func (s *SQLiteStore) GetAll(limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, false)
}

// GetAllBySent returns switches filtered by their processed status.
func (s *SQLiteStore) GetAllBySent(sent bool, limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE sent = ? LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, sent, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query switches by sent status: %w", err)
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, false)
}

// GetByID returns a single switch by its ID, returning sql.ErrNoRows if not found.
func (s *SQLiteStore) GetByID(id int) (api.Switch, error) {
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

// GetExpired returns switches that have timed out and are ready for notification.
func (s *SQLiteStore) GetExpired(limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE sent = ? AND send_at <= ? LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, false, time.Now().UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired switches: %w", err)
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows, true)
}

// Sent marks the switch with the given ID as sent.
func (s *SQLiteStore) Sent(id int) error {
	_, err := s.db.Exec(`UPDATE switches SET sent = ? WHERE id = ?`, true, id)
	return err
}

// Update updates an existing switch's configuration and resets its expiration timer.
func (s *SQLiteStore) Update(id int, sw api.Switch) (api.Switch, error) {
	notifiersJSON, err := json.Marshal(sw.Notifiers)
	if err != nil {
		return api.Switch{}, fmt.Errorf("failed to marshal notifier list: %w", err)
	}

	encrypted := false
	notifiersToStore := string(notifiersJSON)
	messageToStore := sw.Message

	if len(s.EncryptionKey) > 0 && s.EncryptionEnabled {
		encNotifiers, err := s.encrypt(notifiersJSON)
		if err != nil {
			return api.Switch{}, fmt.Errorf("failed to encrypt notifiers: %w", err)
		}

		notifiersToStore = encNotifiers

		if sw.Message != "" {
			encMsg, err := s.encrypt([]byte(sw.Message))
			if err != nil {
				return api.Switch{}, fmt.Errorf("failed to encrypt message: %w", err)
			}

			messageToStore = encMsg
		}

		encrypted = true
	}

	duration, err := time.ParseDuration(sw.CheckInInterval)
	if err != nil {
		return api.Switch{}, fmt.Errorf("invalid checkInInterval: %w", err)
	}

	newSendAt := time.Now().UTC().Add(duration)

	query := `
        UPDATE switches
        SET message = ?,
            notifiers = ?,
            check_in_interval = ?,
            send_at = ?,
            encrypted = ?,
            delete_after_sent = ?,
            sent = 0
        WHERE id = ?`

	result, err := s.db.Exec(query,
		messageToStore,
		notifiersToStore,
		sw.CheckInInterval,
		newSendAt.Format(time.RFC3339),
		encrypted,
		sw.DeleteAfterSent,
		id,
	)
	if err != nil {
		return api.Switch{}, fmt.Errorf("db update failed: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return api.Switch{}, sql.ErrNoRows
	}

	return s.GetByID(id)
}

// Delete permanently removes a switch from the database.
func (s *SQLiteStore) Delete(id int) error {
	_, err := s.db.Exec(`DELETE FROM switches WHERE id = ?`, id)
	return err
}

// Reset resets the send_at time based on the switch's check-in interval and clears the sent status.
func (s *SQLiteStore) Reset(id int) error {
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
	updateQuery := `UPDATE switches SET send_at = ?, sent = 0 WHERE id = ?`

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

// Close closes the connection to the SQLite database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Ping checks if the database connection is still valid.
func (s *SQLiteStore) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.db.PingContext(ctx)
}

// scanSwitches is an internal helper that parses SQL rows into api.Switch structs.
func (s *SQLiteStore) scanSwitches(rows *sql.Rows, plaintext bool) ([]api.Switch, error) {
	var switches []api.Switch

	for rows.Next() {
		sw := api.Switch{}

		var messageRaw sql.NullString
		var notifierRaw string
		var sendAtRaw string
		var encrypted bool

		err := rows.Scan(
			&sw.Id,
			&messageRaw,
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

		if encrypted {
			if plaintext {
				decNotifiers, err := s.decrypt(notifierRaw)
				if err != nil {
					return nil, err
				}

				err = json.Unmarshal(decNotifiers, &sw.Notifiers)
				if err != nil {
					return nil, err
				}

				if messageRaw.Valid {
					decMsg, err := s.decrypt(messageRaw.String)
					if err != nil {
						return nil, err
					}

					sw.Message = string(decMsg)
				}
			} else {
				sw.Message = messageRaw.String
				sw.Notifiers = []string{notifierRaw}
			}
		} else {
			err = json.Unmarshal([]byte(notifierRaw), &sw.Notifiers)
			if err != nil {
				return nil, err
			}

			sw.Message = messageRaw.String
		}

		t, _ := time.Parse(time.RFC3339, sendAtRaw)
		sw.SendAt = &t
		sw.Encrypted = &encrypted
		switches = append(switches, sw)
	}

	return switches, nil
}

// connect is an internal helper that sets up the database connection and directory.
func connect(dbPath string) (*sql.DB, error) {
	err := os.MkdirAll(dbPath, 0755)
	if err != nil {
		return nil, err
	}

	fullPath := filepath.Join(dbPath, dbName)
	params := url.Values{}
	params.Add("_pragma", "journal_mode=WAL")
	params.Add("_pragma", "synchronous=NORMAL")
	params.Add("_busy_timeout", "5000")

	db, err := sql.Open("sqlite", fmt.Sprintf("%s?%s", fullPath, params.Encode()))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	return db, db.Ping()
}
