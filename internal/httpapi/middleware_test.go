package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContentSecurityPolicyAllowsConfiguredS3Origin(t *testing.T) {
	policy := contentSecurityPolicy("http://127.0.0.1:9000/storage/path")
	if !strings.Contains(policy, "connect-src 'self' https: http://127.0.0.1:9000") {
		t.Fatalf("configured S3 origin missing from policy: %s", policy)
	}
}

func TestContentSecurityPolicyRejectsInvalidS3Origin(t *testing.T) {
	policy := contentSecurityPolicy("javascript:alert(1)")
	if strings.Contains(policy, "javascript") {
		t.Fatalf("unsafe S3 endpoint leaked into policy: %s", policy)
	}
}

func TestClientIPOnlyTrustsConfiguredProxy(t *testing.T) {
	request := httptest.NewRequest("GET", "http://sharepipe.test/", nil)
	request.RemoteAddr = "127.0.0.1:43120"
	request.Header.Set("CF-Connecting-IP", "203.0.113.9")

	untrusted := &Server{}
	if got := untrusted.clientIP(request); got != "127.0.0.1" {
		t.Fatalf("untrusted forwarding header accepted: %s", got)
	}

	trusted := &Server{trusted: trustedProxyNetworks([]string{"127.0.0.1/32"})}
	if got := trusted.clientIP(request); got != "203.0.113.9" {
		t.Fatalf("trusted Cloudflare address not accepted: %s", got)
	}
}

func TestHTTPSRedirectOnlyUsesTrustedForwardedScheme(t *testing.T) {
	server := &Server{
		secure:     true,
		publicHost: "https://share.example.test",
		trusted:    trustedProxyNetworks([]string{"127.0.0.1/32"}),
	}
	next := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	handler := server.httpsRedirect(next)

	trusted := httptest.NewRequest(http.MethodPost, "http://share.example.test/path?q=1", nil)
	trusted.RemoteAddr = "127.0.0.1:43000"
	trusted.Header.Set("X-Forwarded-Proto", "http")
	trustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(trustedResponse, trusted)
	if trustedResponse.Code != http.StatusPermanentRedirect {
		t.Fatalf("trusted HTTP request status = %d", trustedResponse.Code)
	}
	if location := trustedResponse.Header().Get("Location"); location != "https://share.example.test/path?q=1" {
		t.Fatalf("redirect location = %q", location)
	}

	untrusted := httptest.NewRequest(http.MethodGet, "http://share.example.test/path", nil)
	untrusted.RemoteAddr = "192.0.2.10:43000"
	untrusted.Header.Set("X-Forwarded-Proto", "http")
	untrustedResponse := httptest.NewRecorder()
	handler.ServeHTTP(untrustedResponse, untrusted)
	if untrustedResponse.Code != http.StatusNoContent {
		t.Fatalf("spoofed forwarded scheme was trusted: %d", untrustedResponse.Code)
	}

	alreadySecure := httptest.NewRequest(http.MethodPost, "https://share.example.test/path", nil)
	alreadySecure.RemoteAddr = "127.0.0.1:43000"
	alreadySecure.Header.Set("X-Forwarded-Proto", "https")
	alreadySecureResponse := httptest.NewRecorder()
	handler.ServeHTTP(alreadySecureResponse, alreadySecure)
	if alreadySecureResponse.Code != http.StatusNoContent {
		t.Fatalf("trusted HTTPS request status = %d", alreadySecureResponse.Code)
	}
}

func TestForwardedSchemeSupportsCloudflareVisitor(t *testing.T) {
	server := &Server{trusted: trustedProxyNetworks([]string{"127.0.0.1/32"})}
	request := httptest.NewRequest(http.MethodGet, "http://share.example.test/", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	request.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	if scheme := server.forwardedScheme(request); scheme != "https" {
		t.Fatalf("CF-Visitor scheme = %q", scheme)
	}
}

func TestSecureResponsesIncludeScopedHSTS(t *testing.T) {
	server := &Server{secure: true, csp: "default-src 'self'"}
	handler := server.securityHeaders(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://share.example.test/", nil))
	if got := response.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("Strict-Transport-Security = %q", got)
	}

	insecureServer := &Server{csp: "default-src 'self'"}
	insecureResponse := httptest.NewRecorder()
	insecureServer.securityHeaders(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(insecureResponse, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if got := insecureResponse.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("insecure response included Strict-Transport-Security = %q", got)
	}
}

func TestAdminSurfaceDefaultsToDirectLoopbackOnly(t *testing.T) {
	server := &Server{
		adminAccess: "local",
		trusted:     trustedProxyNetworks([]string{"127.0.0.1/32"}),
	}
	handler := server.adminSurface(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))

	local := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/admin/settings", nil)
	local.RemoteAddr = "127.0.0.1:43000"
	localResponse := httptest.NewRecorder()
	handler.ServeHTTP(localResponse, local)
	if localResponse.Code != http.StatusNoContent {
		t.Fatalf("direct loopback administrator request status = %d", localResponse.Code)
	}

	tunneled := httptest.NewRequest(http.MethodGet, "https://share.example.test/api/v1/admin/settings", nil)
	tunneled.RemoteAddr = "127.0.0.1:43001"
	tunneled.Header.Set("CF-Connecting-IP", "203.0.113.20")
	tunneled.Header.Set("CF-Ray", "test-SIN")
	tunneledResponse := httptest.NewRecorder()
	handler.ServeHTTP(tunneledResponse, tunneled)
	if tunneledResponse.Code != http.StatusNotFound {
		t.Fatalf("public administrator request status = %d", tunneledResponse.Code)
	}
}

func TestLocalAdministratorRejectsDNSRebindingHost(t *testing.T) {
	server := &Server{adminAccess: "local"}
	handler := server.adminSurface(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/v1/admin/settings", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("DNS-rebound administrator request status = %d", response.Code)
	}
}

func TestLazyAdministratorAssetsShareTheAdministratorBoundary(t *testing.T) {
	for _, path := range []string{
		"/assets/AdminPage-example.js",
		"/assets/AdminPage-example.css",
	} {
		if !isAdminPath(path) {
			t.Fatalf("administrator asset was not protected: %s", path)
		}
	}
	if isAdminPath("/assets/index-example.js") {
		t.Fatal("public application asset was treated as an administrator asset")
	}
}

func TestAdminSurfaceRejectsUnverifiedCloudflareAccessAssertion(t *testing.T) {
	server := &Server{
		adminAccess: "cloudflare-access",
		trusted:     trustedProxyNetworks([]string{"127.0.0.1/32"}),
	}
	handler := server.adminSurface(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "https://share.example.test/api/v1/admin/settings", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	request.Header.Set("CF-Connecting-IP", "203.0.113.20")
	request.Header.Set("CF-Ray", "test-SIN")
	request.Header.Set("Cf-Access-Jwt-Assertion", "edge-validated-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unverified Access assertion status = %d", response.Code)
	}
}

func TestLocalAdministratorOriginIsAccepted(t *testing.T) {
	server := &Server{publicHost: "https://share.example.test"}
	handler := server.sameOrigin(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/admin/login", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("local administrator origin status = %d", response.Code)
	}
}

func TestLocalPublicUploadOriginIsAccepted(t *testing.T) {
	server := &Server{publicHost: "https://share.example.test"}
	handler := server.sameOrigin(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/shares", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("local public upload origin status = %d", response.Code)
	}
}

func TestLoopbackRequestRejectsDifferentOrigin(t *testing.T) {
	server := &Server{publicHost: "https://share.example.test"}
	handler := server.sameOrigin(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/v1/shares", nil)
	request.RemoteAddr = "127.0.0.1:43000"
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("loopback cross-origin status = %d", response.Code)
	}
}

func TestAdministratorResponsesAreNotIndexedOrCached(t *testing.T) {
	server := &Server{csp: "default-src 'self'"}
	handler := server.securityHeaders(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/admin", nil))
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestShareCookieTTLIsBoundedByTwelveHoursAndShareExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, shareTTL, want string
	}{
		{"one-hour", "1h", "1h"},
		{"one-day", "24h", "12h"},
		{"seven-day", "168h", "12h"},
		{"thirty-day", "720h", "12h"},
	} {
		shareTTL, err := time.ParseDuration(test.shareTTL)
		if err != nil {
			t.Fatal(err)
		}
		want, err := time.ParseDuration(test.want)
		if err != nil {
			t.Fatal(err)
		}
		if got := shareCookieTTL(now, now.Add(shareTTL)); got != want {
			t.Fatalf("%s cookie TTL = %s, want %s", test.name, got, want)
		}
	}
	if got := shareCookieTTL(now, now); got != 0 {
		t.Fatalf("expired share cookie TTL = %s", got)
	}
}
