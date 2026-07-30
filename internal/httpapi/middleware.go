package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/security"
)

type contextKey string

const adminClaimsKey contextKey = "adminClaims"
const adminIdentityKey contextKey = "adminIdentity"

type adminIdentity struct {
	Email string
	Local bool
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "SAMEORIGIN")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		response.Header().Set("Content-Security-Policy", s.csp)
		if isAdminPath(request.URL.Path) {
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		}
		if s.secure {
			response.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(response, request)
	})
}

func isAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/") ||
		path == "/api/v1/admin" || strings.HasPrefix(path, "/api/v1/admin/") ||
		strings.HasPrefix(path, "/assets/AdminPage-")
}

func (s *Server) adminSurface(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		identity, ok := s.adminRequestIdentity(request)
		if !ok {
			writeError(response, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), adminIdentityKey, identity)))
	})
}

func (s *Server) adminRequestAllowed(request *http.Request) bool {
	_, ok := s.adminRequestIdentity(request)
	return ok
}

func (s *Server) adminRequestIdentity(request *http.Request) (adminIdentity, bool) {
	if isDirectLoopbackRequest(request) {
		return adminIdentity{Local: true, Email: "local@sharepipe"}, true
	}
	if s.adminAccess == "public" {
		return adminIdentity{Email: "admin"}, true
	}
	if s.adminAccess != "cloudflare-access" || !s.trustedProxyPeer(request) || s.accessValidator == nil {
		return adminIdentity{}, false
	}
	claims, err := s.accessValidator.Validate(request.Context(), request.Header.Get("Cf-Access-Jwt-Assertion"))
	if err != nil || !s.service.AdminEmailAllowed(request.Context(), claims.Email) {
		return adminIdentity{}, false
	}
	return adminIdentity{Email: claims.Email}, true
}

func isDirectLoopbackRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || !ip.IsLoopback() || !isLoopbackRequestHost(request.Host) {
		return false
	}
	return strings.TrimSpace(request.Header.Get("CF-Connecting-IP")) == "" &&
		strings.TrimSpace(request.Header.Get("CF-Ray")) == ""
}

func isLoopbackRequestHost(raw string) bool {
	host := strings.TrimSpace(raw)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) requestIsSecure(request *http.Request) bool {
	return request.TLS != nil || s.forwardedScheme(request) == "https"
}

func (s *Server) httpsRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if s.secure && s.forwardedScheme(request) == "http" {
			http.Redirect(response, request, s.publicHost+request.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (s *Server) forwardedScheme(request *http.Request) string {
	if !s.trustedProxyPeer(request) {
		return ""
	}
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme := strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
		if scheme == "http" || scheme == "https" {
			return scheme
		}
	}
	var visitor struct {
		Scheme string `json:"scheme"`
	}
	if err := json.Unmarshal([]byte(request.Header.Get("CF-Visitor")), &visitor); err == nil {
		scheme := strings.ToLower(strings.TrimSpace(visitor.Scheme))
		if scheme == "http" || scheme == "https" {
			return scheme
		}
	}
	return ""
}

func contentSecurityPolicy(s3Endpoint string) string {
	connectSources := []string{"'self'", "https:"}
	if endpoint, err := url.Parse(strings.TrimSpace(s3Endpoint)); err == nil &&
		(endpoint.Scheme == "http" || endpoint.Scheme == "https") && endpoint.Host != "" {
		origin := endpoint.Scheme + "://" + endpoint.Host
		if origin != "https:" {
			connectSources = append(connectSources, origin)
		}
	}
	return "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; " +
		"style-src 'self' 'unsafe-inline'; script-src 'self' https://challenges.cloudflare.com; connect-src " +
		strings.Join(connectSources, " ") +
		" https://challenges.cloudflare.com; frame-src 'self' https://challenges.cloudflare.com; object-src 'none'; base-uri 'none'; frame-ancestors 'self'"
}

func trustedProxyNetworks(cidrs []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil {
			result = append(result, network)
		}
	}
	return result
}

func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(response, request)
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" && origin != s.publicHost {
			localOriginAllowed := false
			// A loopback-only instance may intentionally retain its public HTTPS URL
			// while the owner exercises the normal upload UI through 127.0.0.1.
			// The request is still same-origin when the browser Origin exactly matches
			// the loopback Host; forwarded/Tunnel requests never satisfy this check.
			if isDirectLoopbackRequest(request) {
				scheme := "http"
				if request.TLS != nil {
					scheme = "https"
				}
				localOriginAllowed = origin == scheme+"://"+request.Host
			}
			if !localOriginAllowed {
				writeError(response, http.StatusForbidden, "origin_rejected", "请求来源无效")
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		identity, ok := request.Context().Value(adminIdentityKey).(adminIdentity)
		if !ok {
			writeError(response, http.StatusUnauthorized, "admin_required", "请先登录管理后台")
			return
		}
		if identity.Local {
			claims := security.Claims{Subject: identity.Email, Scope: "admin", Expires: time.Now().Add(time.Hour).Unix()}
			next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), adminClaimsKey, claims)))
			return
		}
		cookie, err := request.Cookie("sharepipe_admin")
		if err != nil {
			writeError(response, http.StatusUnauthorized, "admin_required", "请先登录管理后台")
			return
		}
		scope := "admin:" + strconv.Itoa(s.service.AdminPasswordVersion(request.Context()))
		claims, err := s.service.Signer().Verify(cookie.Value, scope)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "admin_required", "管理会话已失效")
			return
		}
		if claims.Subject != identity.Email || (s.adminAccess == "cloudflare-access" && !s.service.AdminEmailAllowed(request.Context(), claims.Subject)) {
			writeError(response, http.StatusUnauthorized, "admin_required", "管理会话身份已失效")
			return
		}
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), adminClaimsKey, claims)))
	})
}

func (s *Server) shareUnlocked(request *http.Request, shareID string) bool {
	cookie, err := request.Cookie(shareCookieName(shareID))
	if err != nil {
		return false
	}
	claims, err := s.service.Signer().Verify(cookie.Value, "share:"+shareID)
	if err != nil {
		return false
	}
	version, err := s.service.SharePasswordVersion(request.Context(), shareID)
	return err == nil && claims.Subject == fmt.Sprintf("%s:%d", shareID, version)
}

func shareCookieName(shareID string) string {
	sum := sha256.Sum256([]byte(shareID))
	return "sp_share_" + hex.EncodeToString(sum[:8])
}

func shareCookieTTL(now, expires time.Time) time.Duration {
	ttl := expires.Sub(now)
	if ttl > 12*time.Hour {
		ttl = 12 * time.Hour
	}
	return ttl
}

func (s *Server) setShareCookie(response http.ResponseWriter, shareID string, passwordVersion int, expires time.Time) error {
	now := time.Now()
	ttl := shareCookieTTL(now, expires)
	if ttl <= 0 {
		return core.ErrExpired
	}
	value, err := s.service.Signer().Sign(fmt.Sprintf("%s:%d", shareID, passwordVersion), "share:"+shareID, ttl)
	if err != nil {
		return err
	}
	http.SetCookie(response, &http.Cookie{
		Name: shareCookieName(shareID), Value: value, Path: "/", Expires: now.Add(ttl),
		MaxAge: int(ttl.Seconds()), HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func (s *Server) clientIP(request *http.Request) string {
	peer := net.ParseIP(remoteIP(request))
	if peer == nil {
		return remoteIP(request)
	}
	if !s.trustedProxyPeer(request) {
		return peer.String()
	}
	if connecting := net.ParseIP(strings.TrimSpace(request.Header.Get("CF-Connecting-IP"))); connecting != nil {
		return connecting.String()
	}
	return peer.String()
}

func (s *Server) trustedProxyPeer(request *http.Request) bool {
	peer := net.ParseIP(remoteIP(request))
	if peer == nil {
		return false
	}
	for _, network := range s.trusted {
		if network.Contains(peer) {
			return true
		}
	}
	return false
}

type rateEntry struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mutex sync.Mutex
	items map[string]rateEntry
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{items: make(map[string]rateEntry)}
}

func (r *rateLimiter) allow(key string, maximum int, window time.Duration) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	now := time.Now()
	item := r.items[key]
	if item.reset.Before(now) {
		item = rateEntry{reset: now.Add(window)}
	}
	if item.count >= maximum {
		return false
	}
	item.count++
	r.items[key] = item
	if len(r.items) > 10000 {
		for itemKey, candidate := range r.items {
			if candidate.reset.Before(now) {
				delete(r.items, itemKey)
			}
		}
	}
	return true
}

func (r *rateLimiter) recordFailure(key string, window time.Duration) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	now := time.Now()
	item := r.items[key]
	if item.reset.Before(now) {
		item = rateEntry{reset: now.Add(window)}
	}
	item.count++
	r.items[key] = item
}

func (r *rateLimiter) failures(key string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	item := r.items[key]
	if item.reset.Before(time.Now()) {
		delete(r.items, key)
		return 0
	}
	return item.count
}

func (r *rateLimiter) clear(key string) { r.mutex.Lock(); defer r.mutex.Unlock(); delete(r.items, key) }

func (s *Server) limit(response http.ResponseWriter, request *http.Request, scope string, maximum int, window time.Duration) bool {
	key := strings.Join([]string{scope, s.clientIP(request)}, ":")
	if s.limiter.allow(key, maximum, window) {
		return true
	}
	response.Header().Set("Retry-After", "60")
	writeError(response, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
	return false
}
