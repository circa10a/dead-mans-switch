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
	switchColumns = `id, message, notifiers, send_at, sent, check_in_interval, delete_after_sent, disabled, encrypted, push_subscription, reminder_enabled, reminder_threshold, reminder_sent`
)

// SQLiteStore is an implementation of the Store interface for SQLite.
type SQLiteStore struct {
	db            *sql.DB
	EncryptionKey []byte
}

// New initializes a new SQLiteStore with optional encryption support.
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
	err := s.EncryptSwitch(&sw)
	if err != nil {
		return api.Switch{}, err
	}

	// Prepare Notifiers for SQL
	// If not encrypted, it's a slice and needs to be JSON
	var notifiers any = sw.Notifiers
	if sw.Encrypted != nil && *sw.Encrypted {
		// If encrypted, EncryptSwitch already turned Notifiers into a
		// 1-element slice containing the ciphertext string
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
			// Already encrypted string pointer
			pushSubscription = sw.PushSubscription.Endpoint
		} else {
			// Needs to be raw JSON
			pushJSON, err := json.Marshal(sw.PushSubscription)
			if err != nil {
				return api.Switch{}, err
			}
			pushSubscription = string(pushJSON)
		}
	}

	query := `INSERT INTO switches (message, notifiers, send_at, sent, check_in_interval, delete_after_sent, disabled, encrypted, push_subscription, reminder_enabled, reminder_threshold, reminder_sent)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.Exec(query,
		sw.Message,
		notifiers,
		sw.SendAt,
		false, // Sent default to false
		sw.CheckInInterval,
		sw.DeleteAfterSent != nil && *sw.DeleteAfterSent,
		sw.Disabled != nil && *sw.Disabled,
		sw.Encrypted != nil && *sw.Encrypted,
		pushSubscription,
		sw.ReminderEnabled != nil && *sw.ReminderEnabled,
		sw.ReminderThreshold,
		false, // ReminderSent default to false
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
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE sent = 0 AND disabled = 0 AND send_at <= ? LIMIT ?", switchColumns), time.Now().Unix(), limit)
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
// Switch data will be decrypted so that can be sent appropriately.
func (s *SQLiteStore) GetEligibleReminders(limit int) ([]api.Switch, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT %s FROM switches WHERE sent = 0 AND reminder_enabled = 1 AND reminder_sent = 0 AND disabled = 0 LIMIT ?", switchColumns), limit)
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
func (s *SQLiteStore) Update(id int, sw api.Switch) (api.Switch, error) {
	// Encrypted fields
	err := s.EncryptSwitch(&sw)
	if err != nil {
		return api.Switch{}, err
	}

	// Prepare Notifiers for SQL
	// If not encrypted, it's a slice and needs to be JSON
	var notifiers any = sw.Notifiers
	if sw.Encrypted != nil && *sw.Encrypted {
		// If encrypted, EncryptSwitch already turned Notifiers into a
		// 1-element slice containing the ciphertext string
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
			// Already encrypted string pointer
			pushSubscription = sw.PushSubscription.Endpoint
		} else {
			// Needs to be raw JSON
			pushJSON, err := json.Marshal(sw.PushSubscription)
			if err != nil {
				return api.Switch{}, err
			}
			pushSubscription = string(pushJSON)
		}
	}

	query := `UPDATE switches SET message=?, notifiers=?, check_in_interval=?, send_at=?, disabled=?, encrypted=?, delete_after_sent=?, sent=0, push_subscription=?, reminder_enabled = ?, reminder_threshold=?, reminder_sent=0 WHERE id=?`

	res, err := s.db.Exec(
		query,
		sw.Message,
		notifiers,
		sw.CheckInInterval,
		sw.SendAt,
		sw.Disabled != nil && *sw.Disabled,
		sw.Encrypted != nil && *sw.Encrypted,
		sw.DeleteAfterSent != nil && *sw.DeleteAfterSent,
		pushSubscription,
		sw.ReminderEnabled != nil && *sw.ReminderEnabled,
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

// ReminderSent marks the reminder on the switch with the given ID as sent.
func (s *SQLiteStore) ReminderSent(id int) (api.Switch, error) {
	_, err := s.db.Exec(`UPDATE switches SET reminder_sent = 1 WHERE id = ?`, id)
	if err != nil {
		return api.Switch{}, fmt.Errorf("failed to mark reminder as sent: %w", err)
	}

	return s.GetByID(id)
}

// Sent marks the switch with the given ID as sent.
func (s *SQLiteStore) Sent(id int) (api.Switch, error) {
	_, err := s.db.Exec(`UPDATE switches SET sent = ? WHERE id = ?`, true, id)
	if err != nil {
		return api.Switch{}, fmt.Errorf("failed to mark switch as sent: %w", err)
	}

	return s.GetByID(id)
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
		// Encrypted fields
		var msgRaw string
		var notifiersRaw string
		var pushRaw sql.NullString
		// Optional fields
		var deleteAfterSent sql.NullBool
		var disabled sql.NullBool
		var encrypted sql.NullBool
		var reminderEnabled sql.NullBool
		var reminderThresholdRaw sql.NullString
		var reminderSent sql.NullBool

		err := rows.Scan(
			&sw.Id,
			&msgRaw,
			&notifiersRaw,
			&sw.SendAt,
			&sw.Sent,
			&sw.CheckInInterval,
			// Optional fields
			&deleteAfterSent,
			&disabled,
			&encrypted,
			&pushRaw,
			&reminderEnabled,
			&reminderThresholdRaw,
			&reminderSent,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		// Optional fields
		if deleteAfterSent.Valid {
			sw.DeleteAfterSent = &deleteAfterSent.Bool
		}
		if disabled.Valid {
			sw.Disabled = &disabled.Bool
		}
		if encrypted.Valid {
			sw.Encrypted = &encrypted.Bool
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

		// Field Mapping
		sw.Message = msgRaw
		if sw.Encrypted != nil && *sw.Encrypted {
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

// EncryptSwitch encrypts sensitive switch fields before storing
func (s *SQLiteStore) EncryptSwitch(sw *api.Switch) error {
	if sw.Encrypted == nil || !*sw.Encrypted {
		return nil
	}

	// Encrypt Message
	encMsg, err := s.encrypt([]byte(sw.Message))
	if err != nil {
		return err
	}
	sw.Message = encMsg

	// Encrypt Notifiers
	notifiersJSON, _ := json.Marshal(sw.Notifiers)
	encNotifiers, err := s.encrypt(notifiersJSON)
	if err != nil {
		return err
	}
	sw.Notifiers = []string{encNotifiers}

	// Encrypt Push
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

// DecryptSwitch decrypts sensitive switch fields in place for use in workers or API responses.
func (s *SQLiteStore) DecryptSwitch(sw *api.Switch) error {
	if sw.Encrypted == nil || !*sw.Encrypted {
		return nil
	}
	// Decrypt Message
	decryptedMessage, err := s.decrypt(sw.Message)
	if err != nil {
		return fmt.Errorf("message decryption failed: %w", err)
	}
	sw.Message = string(decryptedMessage)

	// Decrypt Notifiers
	if len(sw.Notifiers) > 0 {
		decryptedNotifiers, err := s.decrypt(sw.Notifiers[0])
		if err != nil {
			return fmt.Errorf("notifiers decryption failed: %w", err)
		}
		if err := json.Unmarshal(decryptedNotifiers, &sw.Notifiers); err != nil {
			return fmt.Errorf("notifiers unmarshal failed: %w", err)
		}
	}

	// Decrypt Push Subscription
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

func (s *SQLiteStore) encrypt(plaintext []byte) (string, error) {
	if len(s.EncryptionKey) == 0 {
		return "", fmt.Errorf("encryption key not configured")
	}

	block, err := aes.NewCipher(s.EncryptionKey)
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

	// Result is nonce + ciphertext
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func (s *SQLiteStore) decrypt(cryptoText string) ([]byte, error) {
	if len(s.EncryptionKey) == 0 {
		return nil, fmt.Errorf("encryption key not configured")
	}

	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(s.EncryptionKey)
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

// connect is an internal helper that sets up the database connection and directory.
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
