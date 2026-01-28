package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func (s *Store) encrypt(plaintext []byte) (string, error) {
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

func (s *Store) decrypt(cryptoText string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return nil, err
	}

	if len(s.EncryptionKey) == 0 {
		return nil, fmt.Errorf("encryption key not configured")
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
