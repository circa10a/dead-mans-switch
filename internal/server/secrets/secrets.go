package secrets

import (
	"crypto/rand"
	"fmt"
	"os"
)

const defaultKeySize = 32

// LoadOrCreateKey loads a 32-byte encryption key from the specified path,
// or creates a new one if it does not exist.
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			key := make([]byte, defaultKeySize)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}

			if err := os.WriteFile(path, key, 0600); err != nil {
				return nil, err
			}

			return key, nil
		}
		return nil, fmt.Errorf("unexpected error reading key file: %w", err)
	}

	if len(data) != defaultKeySize {
		return nil, fmt.Errorf("invalid key size: expected 32 bytes, got %d", len(data))
	}

	return data, nil
}
