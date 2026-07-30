package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func RandomToken(bytes int) (raw string, digest string, err error) {
	if bytes < 16 {
		return "", "", fmt.Errorf("token entropy must be at least 16 bytes")
	}
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buffer)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
