package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	sqliteDBName = "switches_sqlite.db"
	// switchColumns centralizes the field list to prevent Scan errors
	switchColumns = `id, check_in_interval, delete_after_triggered, encrypted, failure_reason, message, notifiers, push_subscription, reminder_enabled, reminder_sent, reminder_threshold, status, trigger_at, user_id`
)

// getUserID extracts the user ID from a switch, defaulting to "admin".
func getUserID(sw api.Switch) string {
	if sw.UserId != nil && *sw.UserId != "" {
		return *sw.UserId
	}
	return AdminUser
}

// sqliteStore is an implementation of the Store interface for SQLite.
type sqliteStore struct {
	db            *sql.DB
	encryptionKey []byte
}

// NewSQLiteStore initializes a new sqliteStore with optional encryption support.
func NewSQLiteStore(dbPath string) (Store, error) {
	db, err := sqliteConnect(dbPath)
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

	return &sqliteStore{
		db:            db,
		encryptionKey: key,
	}, nil
}

// NewInMemorySQLiteStore initializes an in-memory SQLite store suitable for demo mode.
// No data is persisted to disk and a random encryption key is generated.
func NewInMemorySQLiteStore() (Store, error) {
	params := url.Values{}
	params.Add("_pragma", "journal_mode=WAL")
	params.Add("_pragma", "synchronous=NORMAL")

	db, err := sql.Open("sqlite", fmt.Sprintf(":memory:?%s", params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to open in-memory database: %w", err)
	}

	db.SetMaxOpenConns(1)

	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("failed to ping in-memory database: %w", err)
	}

	// Generate a random 32-byte encryption key
	key := make([]byte, 32)
	_, err = io.ReadFull(rand.Reader, key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	return &sqliteStore{
		db:            db,
		encryptionKey: key,
	}, nil
}

// Init creates the necessary database tables if they do not already exist.
func (s *sqliteStore) Init() error {
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// Create inserts a new switch into the database, handling encryption if a key is present.
func (s *sqliteStore) Create(sw api.Switch) (api.Switch, error) {
	err := s.EncryptSwitch(&sw)
	if err != nil {
		return api.Switch{}, err
	}

	// Prepare Notifiers for SQL
	var notifiers any
	if sw.Encrypted != nil && *sw.Encrypted {
		notifiers = sw.Notifiers[0]
	} else {
		notifiersJSON, err := json.Marshal(sw.Notifiers)
		if err != nil {
			return api.Switch{}, err
		}
		notifiers = string(notifiersJSON)
	}

	// Prepare PushSubscription for SQL
	var pushSubscription any = nil
	if sw.PushSubscription != nil {
		if sw.Encrypted != nil && *sw.Encrypted {
			pushSubscription = sw.PushSubscription.Endpoint
		} else {
			pushJSON, err := json.Marshal(sw.PushSubscription)
			if err != nil {
				return api.Switch{}, err
			}
			pushSubscription = string(pushJSON)
		}
	}

	userID := getUserID(sw)

	query := `INSERT INTO switches (check_in_interval, delete_after_triggered, encrypted, failure_reason, message, notifiers, push_subscription, reminder_enabled, reminder_sent, reminder_threshold, status, trigger_at, user_id)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.Exec(query,
		sw.CheckInInterval,
		sw.DeleteAfterTriggered != nil && *sw.DeleteAfterTriggered,
		sw.Encrypted != nil && *sw.Encrypted,
		sw.FailureReason,
		sw.Message,
		notifiers,
		pushSubscription,
		sw.ReminderEnabled != nil && *sw.ReminderEnabled,
		false, // ReminderSent default to false
		sw.ReminderThreshold,
		sw.Status,
		sw.TriggerAt,
		userID,
	)
	if err != nil {
		return api.Switch{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return api.Switch{}, err
	}

	return s.GetByID(userID, int(id))
}

// GetAll returns all switch records in the database up to the provided limit, scoped to the given user.
func (s *sqliteStore) GetAll(userID string, limit int) ([]api.Switch, error) {
	query := fmt.Sprintf("SELECT %s FROM switches WHERE user_id = ? ORDER BY id DESC LIMIT ?", switchColumns)

	rows, err := s.db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return s.scanSwitches(rows)
}

// GetByID returns a single switch by its ID, scoped to the given user. Returns sql.ErrNoRows if not found.
func (s *sqliteStore) GetByID(userID string, id int) (api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE user_id = ? AND id = ?", switchColumns), userID, id)
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
func (s *sqliteStore) GetExpired(limit int) ([]api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE status = ? AND trigger_at <= ? LIMIT ?", switchColumns), api.SwitchStatusActive, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows)
	if err != nil {
		return nil, err
	}

	for i := range switches {
		err := s.DecryptSwitch(&switches[i])
		if err != nil {
			return nil, err
		}
	}

	return switches, nil
}

// GetEligibleReminders finds switches that are approaching expiry, but haven't been warned yet.
func (s *sqliteStore) GetEligibleReminders(limit int) ([]api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE status = ? AND reminder_enabled = 1 AND reminder_sent = 0 LIMIT ?", switchColumns), api.SwitchStatusActive, limit)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	switches, err := s.scanSwitches(rows)
	if err != nil {
		return nil, err
	}

	for i := range switches {
		err := s.DecryptSwitch(&switches[i])
		if err != nil {
			return nil, err
		}
	}

	return switches, nil
}

// Update updates an existing switch's configuration and resets its expiration timer.
func (s *sqliteStore) Update(id int, sw api.Switch) (api.Switch, error) {
	err := s.EncryptSwitch(&sw)
	if err != nil {
		return api.Switch{}, err
	}

	// Prepare Notifiers for SQL
	var notifiers any
	if sw.Encrypted != nil && *sw.Encrypted {
		notifiers = sw.Notifiers[0]
	} else {
		notifiersJSON, err := json.Marshal(sw.Notifiers)
		if err != nil {
			return api.Switch{}, err
		}
		notifiers = string(notifiersJSON)
	}

	// Prepare PushSubscription for SQL
	var pushSubscription any = nil
	if sw.PushSubscription != nil {
		if sw.Encrypted != nil && *sw.Encrypted {
			pushSubscription = sw.PushSubscription.Endpoint
		} else {
			pushJSON, err := json.Marshal(sw.PushSubscription)
			if err != nil {
				return api.Switch{}, err
			}
			pushSubscription = string(pushJSON)
		}
	}

	userID := getUserID(sw)

	query := `UPDATE switches SET check_in_interval=?, delete_after_triggered=?, encrypted=?, failure_reason=?, message=?, notifiers=?, push_subscription=?, reminder_enabled=?, reminder_sent=?, reminder_threshold=?, status=?, trigger_at=? WHERE id=? AND user_id=?`

	res, err := s.db.Exec(
		query,
		sw.CheckInInterval,
		sw.DeleteAfterTriggered != nil && *sw.DeleteAfterTriggered,
		sw.Encrypted != nil && *sw.Encrypted,
		sw.FailureReason,
		sw.Message,
		notifiers,
		pushSubscription,
		sw.ReminderEnabled != nil && *sw.ReminderEnabled,
		sw.ReminderSent != nil && *sw.ReminderSent,
		sw.ReminderThreshold,
		sw.Status,
		sw.TriggerAt,
		id,
		userID,
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

	return s.GetByID(userID, id)
}

// Delete permanently removes a switch from the database, scoped to the given user.
func (s *sqliteStore) Delete(userID string, id int) error {
	_, err := s.db.Exec(`DELETE FROM switches WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// Close closes the connection to the SQLite database.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// Ping checks if the database connection is still valid.
func (s *sqliteStore) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.db.PingContext(ctx)
}

// scanSwitches is an internal helper that parses SQL rows into api.Switch structs.
func (s *sqliteStore) scanSwitches(rows *sql.Rows) ([]api.Switch, error) {
	switches := []api.Switch{}
	for rows.Next() {
		sw := api.Switch{}
		var msgRaw string
		var notifiersRaw string
		var pushRaw sql.NullString
		var DeleteAfterTriggered sql.NullBool
		var encrypted sql.NullBool
		var failureReasonRaw sql.NullString
		var reminderEnabled sql.NullBool
		var reminderThresholdRaw sql.NullString
		var reminderSent sql.NullBool
		var userIDRaw sql.NullString

		err := rows.Scan(
			&sw.Id,
			&sw.CheckInInterval,
			&DeleteAfterTriggered,
			&encrypted,
			&failureReasonRaw,
			&msgRaw,
			&notifiersRaw,
			&pushRaw,
			&reminderEnabled,
			&reminderSent,
			&reminderThresholdRaw,
			&sw.Status,
			&sw.TriggerAt,
			&userIDRaw,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		// Optional fields
		if DeleteAfterTriggered.Valid {
			sw.DeleteAfterTriggered = &DeleteAfterTriggered.Bool
		}
		if encrypted.Valid {
			sw.Encrypted = &encrypted.Bool
		}
		if failureReasonRaw.Valid {
			sw.FailureReason = &failureReasonRaw.String
		}
		if reminderEnabled.Valid {
			sw.ReminderEnabled = &reminderEnabled.Bool
		}
		if reminderThresholdRaw.Valid {
			val := reminderThresholdRaw.String
			sw.ReminderThreshold = &val
		}
		if reminderSent.Valid {
			sw.ReminderSent = &reminderSent.Bool
		}
		if userIDRaw.Valid {
			sw.UserId = &userIDRaw.String
		}

		// Field Mapping
		sw.Message = msgRaw
		if sw.Encrypted != nil && *sw.Encrypted {
			sw.Notifiers = []string{notifiersRaw}
			if pushRaw.Valid {
				sw.PushSubscription = &api.PushSubscription{
					Endpoint: &pushRaw.String,
				}
			}
		} else {
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

// EncryptSwitch encrypts sensitive switch fields before storing
func (s *sqliteStore) EncryptSwitch(sw *api.Switch) error {
	if sw.Encrypted == nil || !*sw.Encrypted {
		return nil
	}

	encMsg, err := s.encrypt([]byte(sw.Message))
	if err != nil {
		return err
	}
	sw.Message = encMsg

	notifiersJSON, _ := json.Marshal(sw.Notifiers)
	encNotifiers, err := s.encrypt(notifiersJSON)
	if err != nil {
		return err
	}
	sw.Notifiers = []string{encNotifiers}

	if sw.PushSubscription != nil {
		pushJSON, _ := json.Marshal(sw.PushSubscription)
		encPush, err := s.encrypt(pushJSON)
		if err != nil {
			return err
		}
		sw.PushSubscription = &api.PushSubscription{Endpoint: &encPush}
	}

	return nil
}

// DecryptSwitch decrypts sensitive switch fields in place.
func (s *sqliteStore) DecryptSwitch(sw *api.Switch) error {
	if sw.Encrypted == nil || !*sw.Encrypted {
		return nil
	}
	decryptedMessage, err := s.decrypt(sw.Message)
	if err != nil {
		return fmt.Errorf("message decryption failed: %w", err)
	}
	sw.Message = string(decryptedMessage)

	if len(sw.Notifiers) > 0 {
		decryptedNotifiers, err := s.decrypt(sw.Notifiers[0])
		if err != nil {
			return fmt.Errorf("notifiers decryption failed: %w", err)
		}
		if err := json.Unmarshal(decryptedNotifiers, &sw.Notifiers); err != nil {
			return fmt.Errorf("notifiers unmarshal failed: %w", err)
		}
	}

	if sw.PushSubscription != nil && sw.PushSubscription.Endpoint != nil {
		decryptedPush, err := s.decrypt(*sw.PushSubscription.Endpoint)
		if err != nil {
			return fmt.Errorf("push decryption failed: %w", err)
		}
		if err := json.Unmarshal(decryptedPush, &sw.PushSubscription); err != nil {
			return fmt.Errorf("push unmarshal failed: %w", err)
		}
	}

	return nil
}

func (s *sqliteStore) encrypt(plaintext []byte) (string, error) {
	if len(s.encryptionKey) == 0 {
		return "", fmt.Errorf("encryption key not configured")
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())

	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func (s *sqliteStore) decrypt(cryptoText string) ([]byte, error) {
	if len(s.encryptionKey) == 0 {
		return nil, fmt.Errorf("encryption key not configured")
	}

	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

func sqliteConnect(dbPath string) (*sql.DB, error) {
	err := os.MkdirAll(dbPath, 0750)
	if err != nil {
		return nil, err
	}

	fullPath := filepath.Join(dbPath, sqliteDBName)
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
