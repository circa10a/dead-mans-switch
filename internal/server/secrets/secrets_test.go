package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	t.Run("creates a new key if none exists", func(t *testing.T) {
		exists, key, err := LoadOrCreateKey(keyPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify boolean logic: Should be false for new key
		if exists {
			t.Error("expected exists to be false for a newly created key")
		}

		if len(key) != defaultKeySize {
			t.Errorf("expected %d bytes, got %d", defaultKeySize, len(key))
		}

		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			t.Error("expected key file to be created on disk")
		}
	})

	t.Run("loads existing key from disk", func(t *testing.T) {
		// Read the key created in the previous test to compare
		originalKey, _ := os.ReadFile(keyPath)

		exists, loadedKey, err := LoadOrCreateKey(keyPath)
		if err != nil {
			t.Fatalf("expected no error loading existing key, got %v", err)
		}

		// Verify boolean logic: Should be true for existing key
		if !exists {
			t.Error("expected exists to be true for an existing key")
		}

		// Verify it matches what was on disk (didn't regenerate)
		if string(originalKey) != string(loadedKey) {
			t.Error("expected loaded key to match original key, but it was different")
		}
	})

	t.Run("returns error for invalid key size", func(t *testing.T) {
		badKeyPath := filepath.Join(tmpDir, "bad.key")
		// Write a key that is too short (15 bytes instead of 32)
		_ = os.WriteFile(badKeyPath, []byte("too-short-key!!"), 0600)

		_, _, err := LoadOrCreateKey(badKeyPath)
		if err == nil {
			t.Error("expected error for invalid key size, got nil")
		}
	})

	t.Run("enforces restrictive permissions on new files", func(t *testing.T) {
		permKeyPath := filepath.Join(tmpDir, "perm.key")
		_, _, _ = LoadOrCreateKey(permKeyPath)

		info, err := os.Stat(permKeyPath)
		if err != nil {
			t.Fatal(err)
		}

		// 0600 = -rw-------
		expectedPerm := os.FileMode(0600)
		if info.Mode().Perm() != expectedPerm {
			t.Errorf("expected permissions %v, got %v", expectedPerm, info.Mode().Perm())
		}
	})
}
