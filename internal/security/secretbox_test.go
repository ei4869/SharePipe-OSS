package security

import (
	"bytes"
	"testing"
)

func TestSecretBoxRoundTripAndPurposeBinding(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	encoded, err := EncryptSecret(key, []byte("credential"), "storage:one")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptSecret(key, encoded, "storage:one")
	if err != nil || string(plain) != "credential" {
		t.Fatalf("round trip failed: %q %v", plain, err)
	}
	if _, err := DecryptSecret(key, encoded, "storage:two"); err == nil {
		t.Fatal("ciphertext was accepted for a different purpose")
	}
}
