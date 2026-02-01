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

func TestLoadOrCreateVAPIDKeys(t *testing.T) {
	tmpDir := t.TempDir()
	privPath := filepath.Join(tmpDir, "vapid.priv")
	pubPath := filepath.Join(tmpDir, "vapid.pub")

	t.Run("generates new VAPID keys if they do not exist", func(t *testing.T) {
		priv, pub, err := LoadOrCreateVAPIDKeys(privPath, pubPath)
		if err != nil {
			t.Fatalf("Failed to generate VAPID keys: %v", err)
		}

		if priv == "" || pub == "" {
			t.Error("expected non-empty strings for VAPID keys")
		}

		// Check if files exist
		if _, err := os.Stat(privPath); os.IsNotExist(err) {
			t.Error("private key file was not created")
		}
		if _, err := os.Stat(pubPath); os.IsNotExist(err) {
			t.Error("public key file was not created")
		}
	})

	t.Run("loads existing VAPID keys from disk", func(t *testing.T) {
		// Read keys generated in previous step
		origPriv, _ := os.ReadFile(privPath)
		origPub, _ := os.ReadFile(pubPath)

		priv, pub, err := LoadOrCreateVAPIDKeys(privPath, pubPath)
		if err != nil {
			t.Fatalf("Failed to load existing VAPID keys: %v", err)
		}

		if priv != string(origPriv) || pub != string(origPub) {
			t.Error("loaded keys do not match the keys stored on disk")
		}
	})

	t.Run("enforces restrictive permissions on private key", func(t *testing.T) {
		info, err := os.Stat(privPath)
		if err != nil {
			t.Fatal(err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 permissions on private key, got %v", info.Mode().Perm())
		}
	})
}
