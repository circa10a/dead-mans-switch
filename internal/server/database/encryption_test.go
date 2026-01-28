package database

import (
	"bytes"
	"testing"
)

func TestEncryptionRoundTrip(t *testing.T) {
	// AES-256 requires a 32-byte key
	key := []byte("this-is-a-32-byte-long-test-key!")
	store := &SQLiteStore{
		EncryptionKey: key,
	}

	plaintext := []byte("Hello, Dead Man's Switch!")

	t.Run("successfully encrypts and decrypts", func(t *testing.T) {
		ciphertext, err := store.encrypt(plaintext)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		if ciphertext == string(plaintext) {
			t.Error("ciphertext should not match plaintext")
		}

		decrypted, err := store.decrypt(ciphertext)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("expected %s, got %s", string(plaintext), string(decrypted))
		}
	})

	t.Run("fails when key is missing", func(t *testing.T) {
		emptyStore := &SQLiteStore{EncryptionKey: nil}

		_, err := emptyStore.encrypt(plaintext)
		if err == nil {
			t.Error("expected error when encrypting without a key")
		}

		_, err = emptyStore.decrypt("some-data")
		if err == nil {
			t.Error("expected error when decrypting without a key")
		}
	})

	t.Run("randomized output for same input", func(t *testing.T) {
		// Encrypt the same thing twice
		c1, _ := store.encrypt(plaintext)
		c2, _ := store.encrypt(plaintext)

		if c1 == c2 {
			t.Error("ciphertexts should be unique due to random nonce")
		}
	})

	t.Run("fails with invalid base64 input", func(t *testing.T) {
		_, err := store.decrypt("not-base64-!!!")
		if err == nil {
			t.Error("expected error for invalid base64 input")
		}
	})

	t.Run("fails with tampered ciphertext", func(t *testing.T) {
		ciphertext, _ := store.encrypt(plaintext)
		// Alter one character in the middle of the base64 string
		tampered := ciphertext[:10] + "A" + ciphertext[11:]

		_, err := store.decrypt(tampered)
		if err == nil {
			t.Error("expected error for tampered ciphertext (GCM authentication should fail)")
		}
	})
}
