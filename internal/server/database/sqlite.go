package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/circa10a/dead-mans-switch/internal/server/secrets"

	// Import the sqlite driver that requires no CGO deps
	_ "modernc.org/sqlite"
)

const (
	dbName     = "switches.db"
	secretName = "switches_encryption.key"
	// switchColumns centralizes the field list to prevent Scan errors
	switchColumns = `id, message, notifiers, send_at, sent, check_in_interval, delete_after_sent, disabled, encrypted, push_subscription, reminder_threshold, reminder_sent`
)

// SQLiteStore is an implementation of the Store interface for SQLite.
type SQLiteStore struct {
	db            *sql.DB
	EncryptionKey []byte
}

// New initializes a new SQLiteStore with optional encryption support.
func New(dbPath string) (Store, error) {
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

	if len(key) == 0 {
		return nil, errors.New("encryption key content must be more than 0 bytes")
	}

	return &SQLiteStore{
		db:            db,
		EncryptionKey: key,
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
	msg, notifiers, push, err := s.prepareSwitchForStorage(sw)
	if err != nil {
		return api.Switch{}, err
	}

	// TODO Handle in handler
	checkInInterval, err := time.ParseDuration(sw.CheckInInterval)
	if err != nil {
		return api.Switch{}, fmt.Errorf("invalid checkInInterval: %w", err)
	}

	if sw.ReminderThreshold != nil {
		_, err = time.ParseDuration(*sw.ReminderThreshold)
		if err != nil {
			return api.Switch{}, fmt.Errorf("invalid reminderThreshold: %w", err)
		}
	}

	// TODO handle in handler
	sendAt := time.Now().UTC().Add(checkInInterval)
	query := `INSERT INTO switches (message, notifiers, send_at, sent, check_in_interval, delete_after_sent, disabled, encrypted, push_subscription, reminder_threshold, reminder_sent)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.Exec(query,
		msg,
		notifiers,
		sendAt.Format(time.RFC3339),
		false,
		sw.CheckInInterval,
		sw.DeleteAfterSent,
		sw.Disabled,
		sw.Encrypted,
		push,
		sw.ReminderThreshold,
		false,
	)
	if err != nil {
		return api.Switch{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return api.Switch{}, err
	}

	return s.GetByID(int(id))
}

// GetAll returns all switch records in the database up to the provided limit.
func (s *SQLiteStore) GetAll(limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches ORDER BY id DESC LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows)
}

// GetByID returns a single switch by its ID, returning sql.ErrNoRows if not found.
func (s *SQLiteStore) GetByID(id int) (api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE id = ?", switchColumns), id)
	if err != nil {
		return api.Switch{}, err
	}

	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows)
	if err != nil {
		return api.Switch{}, err
	}

	if len(switches) == 0 {
		return api.Switch{}, sql.ErrNoRows
	}

	return switches[0], nil
}

// GetExpired returns switches that have timed out and are ready for notification.
// Switch data will be decrypted so that can be sent appropriately.
func (s *SQLiteStore) GetExpired(limit int) ([]api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE sent = 0 AND disabled = 0 AND send_at <= ? LIMIT ?", switchColumns), time.Now().UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows)
	if err != nil {
		return nil, err
	}

	for i := range switches {
		err := s.decryptInPlace(&switches[i])
		if err != nil {
			return nil, err
		}
	}

	return switches, nil
}

// GetEligibleReminders finds switches that are approaching expiry, but haven't been warned yet.
// Switch data will be decrypted so that can be sent appropriately.
func (s *SQLiteStore) GetEligibleReminders(limit int) ([]api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE sent = 0 AND reminder_sent = 0 AND disabled = 0 AND reminder_threshold IS NOT NULL LIMIT ?", switchColumns), limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows)
	if err != nil {
		return nil, err
	}

	for i := range switches {
		err := s.decryptInPlace(&switches[i])
		if err != nil {
			return nil, err
		}
	}

	return switches, nil
}

// ReminderSent marks the reminder on the switch with the given ID as sent.
func (s *SQLiteStore) ReminderSent(id int) error {
	_, err := s.db.Exec(`UPDATE switches SET reminder_sent = 1 WHERE id = ?`, id)
	return err
}

// Sent marks the switch with the given ID as sent.
func (s *SQLiteStore) Sent(id int) error {
	_, err := s.db.Exec(`UPDATE switches SET sent = ? WHERE id = ?`, true, id)
	return err
}

// Update updates an existing switch's configuration and resets its expiration timer.
func (s *SQLiteStore) Update(id int, sw api.Switch) (api.Switch, error) {
	// Encrypted fields
	msg, notifiers, push, err := s.prepareSwitchForStorage(sw)
	if err != nil {
		return api.Switch{}, err
	}

	// TODO, handle in update handler
	duration, err := time.ParseDuration(sw.CheckInInterval)
	if err != nil {
		return api.Switch{}, err
	}

	sendAt := time.Now().UTC().Add(duration)
	query := `UPDATE switches SET message=?, notifiers=?, check_in_interval=?, send_at=?, disabled=?, encrypted=?, delete_after_sent=?, sent=0, push_subscription=?, reminder_threshold=?, reminder_sent=0 WHERE id=?`

	res, err := s.db.Exec(
		query,
		msg,
		notifiers,
		sw.CheckInInterval,
		sendAt.Format(time.RFC3339),
		sw.Disabled,
		sw.Encrypted,
		sw.DeleteAfterSent,
		push,
		sw.ReminderThreshold,
		id,
	)
	if err != nil {
		return api.Switch{}, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return api.Switch{}, err
	}

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

// Reset resets the send_at time based on the switch's check-in interval and clears the reminder_sent/sent status.
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
	updateQuery := `UPDATE switches SET disabled = 0, send_at = ?, sent = 0, reminder_sent = 0 WHERE id = ?`

	result, err := s.db.Exec(updateQuery, newSendAt.Format(time.RFC3339), id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Disable sets the disabled status of a switch to true.
func (s *SQLiteStore) Disable(id int) error {
	query := `UPDATE switches SET disabled = 1 WHERE id = ?`
	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to disable switch: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

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
func (s *SQLiteStore) scanSwitches(rows *sql.Rows) ([]api.Switch, error) {
	switches := []api.Switch{}
	for rows.Next() {
		sw := api.Switch{}
		var msgRaw string
		var notifiersRaw string
		var sendAtRaw string
		var pushRaw sql.NullString
		var remindIntRaw sql.NullString
		var reminderSentBool bool

		err := rows.Scan(
			&sw.Id,
			&msgRaw,
			&notifiersRaw,
			&sendAtRaw,
			&sw.Sent,
			&sw.CheckInInterval,
			&sw.DeleteAfterSent,
			&sw.Disabled,
			&sw.Encrypted,
			&pushRaw,
			&remindIntRaw,
			&reminderSentBool,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		// Basic fields
		if remindIntRaw.Valid {
			val := remindIntRaw.String
			sw.ReminderThreshold = &val
		}
		sw.ReminderSent = &reminderSentBool
		sendAt, err := time.Parse(time.RFC3339, sendAtRaw)
		if err != nil {
			return nil, err
		}

		sw.SendAt = &sendAt

		// Field Mapping
		sw.Message = msgRaw
		if sw.Encrypted {
			// Encrypted - Treat raw DB strings as the "API View" data
			sw.Notifiers = []string{notifiersRaw}
			if pushRaw.Valid {
				sw.PushSubscription = &api.PushSubscription{
					Endpoint: &pushRaw.String,
				}
			}
		} else {
			// Plaintext - Unmarshal JSON blobs
			err = json.Unmarshal([]byte(notifiersRaw), &sw.Notifiers)
			if err != nil {
				return nil, err
			}

			if pushRaw.Valid && pushRaw.String != "" {
				err = json.Unmarshal([]byte(pushRaw.String), &sw.PushSubscription)
				if err != nil {
					return nil, err
				}
			}
		}
		switches = append(switches, sw)
	}

	return switches, nil
}

// prepareSwitchForStorage encrypts sensitive switch fields before storing
func (s *SQLiteStore) prepareSwitchForStorage(sw api.Switch) (string, string, sql.NullString, error) {
	var msgStore string
	var notifiersStore string
	var pushStore sql.NullString

	// Marshal Notifiers
	notifiersJSON, err := json.Marshal(sw.Notifiers)
	if err != nil {
		return "", "", pushStore, fmt.Errorf("marshal notifiers: %w", err)
	}
	notifiersStore = string(notifiersJSON)
	msgStore = sw.Message

	// Marshal Push Sub
	if sw.PushSubscription != nil && sw.PushSubscription.Endpoint != nil {
		pushJSON, err := json.Marshal(sw.PushSubscription)
		if err != nil {
			return "", "", pushStore, fmt.Errorf("marshal push: %w", err)
		}
		pushStore = sql.NullString{String: string(pushJSON), Valid: true}
	}

	// Encrypt if requested
	if sw.Encrypted {
		encMsg, err := s.encrypt([]byte(sw.Message))
		if err != nil {
			return "", "", pushStore, fmt.Errorf("encrypt message: %w", err)
		}
		msgStore = encMsg

		encNotifiers, err := s.encrypt(notifiersJSON)
		if err != nil {
			return "", "", pushStore, fmt.Errorf("encrypt notifiers: %w", err)
		}
		notifiersStore = encNotifiers

		if pushStore.Valid {
			encPush, err := s.encrypt([]byte(pushStore.String))
			if err != nil {
				return "", "", pushStore, fmt.Errorf("encrypt push: %w", err)
			}
			pushStore.String = encPush
		}
	}

	return msgStore, notifiersStore, pushStore, nil
}

// decryptInPlace decrypts sensitive switch fields for the worker to properly send notifications.
func (s *SQLiteStore) decryptInPlace(sw *api.Switch) error {
	if !sw.Encrypted {
		return nil
	}

	// Decrypt Message
	decryptedMessage, err := s.decrypt(sw.Message)
	if err != nil {
		return fmt.Errorf("msg decryption: %w", err)
	}
	sw.Message = string(decryptedMessage)

	// Decrypt Notifiers (Expected to be the first element in API view)
	if len(sw.Notifiers) > 0 {
		decryptedNotifiers, err := s.decrypt(sw.Notifiers[0])
		if err != nil {
			return fmt.Errorf("notifiers decryption: %w", err)
		}
		err = json.Unmarshal(decryptedNotifiers, &sw.Notifiers)
		if err != nil {
			return fmt.Errorf("notifiers unmarshal: %w", err)
		}
	}

	// Decrypt Push Subscription
	if sw.PushSubscription != nil && sw.PushSubscription.Endpoint != nil {
		decryptedPushSubscription, err := s.decrypt(*sw.PushSubscription.Endpoint)
		if err != nil {
			return fmt.Errorf("push decryption: %w", err)
		}
		err = json.Unmarshal(decryptedPushSubscription, &sw.PushSubscription)
		if err != nil {
			return fmt.Errorf("push unmarshal: %w", err)
		}
	}

	return nil
}

// connect is an internal helper that sets up the database connection and directory.
func connect(dbPath string) (*sql.DB, error) {
	err := os.MkdirAll(dbPath, 0750)
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
