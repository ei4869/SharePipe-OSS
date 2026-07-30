package security

import (
	"testing"
	"time"
)

func TestRandomTokenAndDigest(t *testing.T) {
	raw, digest, err := RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || digest != HashToken(raw) {
		t.Fatal("token digest mismatch")
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(encoded, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("password should verify: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword(encoded, "incorrect password")
	if err != nil || ok {
		t.Fatalf("wrong password should not verify: ok=%v err=%v", ok, err)
	}
}

func TestSigner(t *testing.T) {
	signer, err := NewSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	signer.now = func() time.Time { return now }
	token, err := signer.Sign("share-1", "share", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(token, "share")
	if err != nil || claims.Subject != "share-1" {
		t.Fatalf("session should verify: %#v %v", claims, err)
	}
	if _, err := signer.Verify(token, "admin"); err == nil {
		t.Fatal("scope mismatch should fail")
	}
	signer.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := signer.Verify(token, "share"); err == nil {
		t.Fatal("expired session should fail")
	}
}
