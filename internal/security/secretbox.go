package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const secretBoxVersion = "v1"

// EncryptSecret protects application credentials with AES-256-GCM. The
// caller-supplied purpose is authenticated as associated data so ciphertext
// cannot be moved between storage profiles without detection.
func EncryptSecret(key, plaintext []byte, purpose string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("secret encryption key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(purpose))
	payload := append(nonce, ciphertext...)
	return secretBoxVersion + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecryptSecret(key []byte, encoded, purpose string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("secret encryption key must contain exactly 32 bytes")
	}
	parts := strings.SplitN(encoded, ".", 2)
	if len(parts) != 2 || parts[0] != secretBoxVersion {
		return nil, errors.New("unsupported encrypted secret format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid encrypted secret encoding")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted secret is truncated")
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte(purpose))
	if err != nil {
		return nil, errors.New("encrypted secret authentication failed")
	}
	return plaintext, nil
}
