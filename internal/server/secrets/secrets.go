package secrets

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SherClockHolmes/webpush-go"
)

const defaultKeySize = 32

// LoadOrCreateKey loads a 32-byte encryption key from the specified path,
// or creates a new one if it does not exist.
// Returns (true, key, nil) if loaded from disk, (false, key, nil) if newly created.
func LoadOrCreateKey(path string) (bool, []byte, error) {
	// Clean the path to remove ../ and other traversal shortcuts
	cleanPath := filepath.Clean(path)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			key := make([]byte, defaultKeySize)
			_, err := rand.Read(key)
			if err != nil {
				return false, nil, fmt.Errorf("failed to generate random key: %w", err)
			}

			err = os.WriteFile(cleanPath, key, 0600)
			if err != nil {
				return false, nil, fmt.Errorf("failed to write new key file: %w", err)
			}

			// Return false because it was created, not loaded
			return false, key, nil
		}

		return false, nil, fmt.Errorf("unexpected error reading key file: %w", err)
	}

	if len(data) != defaultKeySize {
		return false, nil, fmt.Errorf("invalid key size: expected 32 bytes, got %d", len(data))
	}

	// Return true because it existed and was readable
	return true, data, nil
}

// LoadOrCreateVAPIDKeys handles the specific ECDSA keys required for Web Push.
// Returns (privateKey, publicKey, error)
func LoadOrCreateVAPIDKeys(privPath, pubPath string) (string, string, error) {
	privPath = filepath.Clean(privPath)
	pubPath = filepath.Clean(pubPath)

	privBuf, errPriv := os.ReadFile(privPath)
	pubBuf, errPub := os.ReadFile(pubPath)

	if errPriv == nil && errPub == nil {
		return string(privBuf), string(pubBuf), nil
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}

	err = os.WriteFile(privPath, []byte(privateKey), 0600)
	if err != nil {
		return "", "", err
	}

	err = os.WriteFile(pubPath, []byte(publicKey), 0600)
	if err != nil {
		return "", "", err
	}

	return privateKey, publicKey, nil
}
