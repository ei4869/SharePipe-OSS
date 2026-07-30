package security

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccessValidatorVerifiesSignatureAudienceIssuerAndEmail(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "test-key"
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(response, request)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"keys": []map[string]string{{
			"kid": keyID, "kty": "RSA",
			"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
		}}})
	}))
	defer server.Close()

	validator, err := NewAccessValidator(server.URL, "sharepipe-audience", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claims := AccessClaims{
		Subject: "user-id", Email: "Admin@Example.com", Issuer: server.URL,
		Audience: Audience{"sharepipe-audience"}, Expires: now.Add(time.Hour).Unix(),
		IssuedAt: now.Unix(), Type: "app",
	}
	token := signAccessTestToken(t, privateKey, keyID, claims)
	actual, err := validator.Validate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Email != "admin@example.com" {
		t.Fatalf("normalized email = %q", actual.Email)
	}

	claims.Audience = Audience{"another-app"}
	if _, err := validator.Validate(context.Background(), signAccessTestToken(t, privateKey, keyID, claims)); err == nil {
		t.Fatal("wrong Access audience was accepted")
	}
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"forged","email":"attacker@example.com"}`))
	if _, err := validator.Validate(context.Background(), strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered Access token was accepted")
	}
}

func TestAccessValidatorRejectsNonOriginTeamDomain(t *testing.T) {
	for _, value := range []string{
		"http://team.cloudflareaccess.com",
		"https://user@team.cloudflareaccess.com",
		"https://team.cloudflareaccess.com/path",
		"https://team.cloudflareaccess.com?query=value",
		"https://team.cloudflareaccess.com#fragment",
	} {
		if _, err := NewAccessValidator(value, "audience", nil); err == nil {
			t.Fatalf("invalid team domain was accepted: %s", value)
		}
	}
}

func signAccessTestToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims AccessClaims) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": keyID, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
