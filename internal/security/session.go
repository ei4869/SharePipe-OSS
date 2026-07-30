package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Claims struct {
	Subject string `json:"sub"`
	Scope   string `json:"scope"`
	Expires int64  `json:"exp"`
}

type Signer struct {
	key []byte
	now func() time.Time
}

func NewSigner(key []byte) (*Signer, error) {
	if len(key) < 32 {
		return nil, errors.New("session signing key must contain at least 32 bytes")
	}
	copyKey := append([]byte(nil), key...)
	return &Signer{key: copyKey, now: time.Now}, nil
}

func (s *Signer) Sign(subject, scope string, ttl time.Duration) (string, error) {
	claims := Claims{Subject: subject, Scope: scope, Expires: s.now().Add(ttl).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (s *Signer) Verify(token, scope string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, errors.New("invalid session token")
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(parts[0]))
	expected, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return Claims{}, errors.New("invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, errors.New("invalid session payload")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, errors.New("invalid session claims")
	}
	if claims.Scope != scope {
		return Claims{}, errors.New("invalid session scope")
	}
	if s.now().Unix() >= claims.Expires {
		return Claims{}, errors.New("session expired")
	}
	return claims, nil
}
