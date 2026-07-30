package security

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AccessClaims struct {
	Subject   string   `json:"sub"`
	Email     string   `json:"email"`
	Issuer    string   `json:"iss"`
	Audience  Audience `json:"aud"`
	Expires   int64    `json:"exp"`
	NotBefore int64    `json:"nbf,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	Type      string   `json:"type,omitempty"`
}

type Audience []string

func (a *Audience) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*a = values
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("invalid JWT audience")
	}
	*a = []string{value}
	return nil
}

type AccessValidator struct {
	issuer   string
	audience string
	certsURL string
	client   *http.Client
	now      func() time.Time

	mutex       sync.Mutex
	keys        map[string]*rsa.PublicKey
	keysExpires time.Time
}

func NewAccessValidator(teamDomain, audience string, client *http.Client) (*AccessValidator, error) {
	teamDomain = strings.TrimRight(strings.TrimSpace(teamDomain), "/")
	parsed, err := url.Parse(teamDomain)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Cloudflare Access team domain must be an HTTPS origin")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, errors.New("Cloudflare Access audience is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &AccessValidator{
		issuer: teamDomain, audience: strings.TrimSpace(audience),
		certsURL: teamDomain + "/cdn-cgi/access/certs", client: client,
		now: time.Now, keys: make(map[string]*rsa.PublicKey),
	}, nil
}

func (v *AccessValidator) Validate(ctx context.Context, token string) (AccessClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return AccessClaims{}, errors.New("invalid Cloudflare Access token")
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := decodeJWTPart(parts[0], &header); err != nil || header.Algorithm != "RS256" || header.KeyID == "" {
		return AccessClaims{}, errors.New("invalid Cloudflare Access token header")
	}
	var claims AccessClaims
	if err := decodeJWTPart(parts[1], &claims); err != nil {
		return AccessClaims{}, errors.New("invalid Cloudflare Access token claims")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessClaims{}, errors.New("invalid Cloudflare Access token signature")
	}
	key, err := v.key(ctx, header.KeyID, false)
	if err != nil {
		key, err = v.key(ctx, header.KeyID, true)
		if err != nil {
			return AccessClaims{}, err
		}
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		// Cloudflare may have rotated its signing keys since our last refresh.
		if refreshed, refreshErr := v.key(ctx, header.KeyID, true); refreshErr == nil {
			key = refreshed
		}
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
			return AccessClaims{}, errors.New("Cloudflare Access token signature is invalid")
		}
	}
	now := v.now().Unix()
	if claims.Issuer != v.issuer || !claims.Audience.Contains(v.audience) {
		return AccessClaims{}, errors.New("Cloudflare Access token issuer or audience is invalid")
	}
	if claims.Expires == 0 || now >= claims.Expires || (claims.NotBefore != 0 && now+60 < claims.NotBefore) {
		return AccessClaims{}, errors.New("Cloudflare Access token is expired or not active")
	}
	if claims.Type != "" && claims.Type != "app" {
		return AccessClaims{}, errors.New("Cloudflare Access token type is invalid")
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Subject == "" || claims.Email == "" || !strings.Contains(claims.Email, "@") {
		return AccessClaims{}, errors.New("Cloudflare Access token has no user identity")
	}
	return claims, nil
}

func (a Audience) Contains(expected string) bool {
	for _, value := range a {
		if value == expected {
			return true
		}
	}
	return false
}

func decodeJWTPart(encoded string, target any) error {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func (v *AccessValidator) key(ctx context.Context, keyID string, force bool) (*rsa.PublicKey, error) {
	v.mutex.Lock()
	defer v.mutex.Unlock()
	if !force && v.now().Before(v.keysExpires) {
		if key := v.keys[keyID]; key != nil {
			return key, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Cloudflare Access signing keys: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch Cloudflare Access signing keys: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read Cloudflare Access signing keys: %w", err)
	}
	var set struct {
		Keys []struct {
			KeyID string `json:"kid"`
			Type  string `json:"kty"`
			N     string `json:"n"`
			E     string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, errors.New("decode Cloudflare Access signing keys")
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, item := range set.Keys {
		if item.KeyID == "" || item.Type != "RSA" {
			continue
		}
		modulus, modulusErr := base64.RawURLEncoding.DecodeString(item.N)
		exponent, exponentErr := base64.RawURLEncoding.DecodeString(item.E)
		if modulusErr != nil || exponentErr != nil || len(exponent) == 0 || len(exponent) > 4 {
			continue
		}
		e := 0
		for _, value := range exponent {
			e = e<<8 | int(value)
		}
		if e < 3 {
			continue
		}
		keys[item.KeyID] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: e}
	}
	v.keys = keys
	v.keysExpires = v.now().Add(6 * time.Hour)
	if key := keys[keyID]; key != nil {
		return key, nil
	}
	return nil, errors.New("Cloudflare Access signing key was not found")
}
