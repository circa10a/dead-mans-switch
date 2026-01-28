package secrets

import (
	"crypto/rand"
	"fmt"
	"os"
)

const defaultKeySize = 32

// LoadOrCreateKey loads a 32-byte encryption key from the specified path,
// or creates a new one if it does not exist.
// Returns (true, key, nil) if loaded from disk, (false, key, nil) if newly created.
func LoadOrCreateKey(path string) (bool, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			key := make([]byte, defaultKeySize)
			_, err := rand.Read(key)
			if err != nil {
				return false, nil, fmt.Errorf("failed to generate random key: %w", err)
			}

			err = os.WriteFile(path, key, 0600)
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
