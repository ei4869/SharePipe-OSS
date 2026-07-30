package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/service"
)

const trustedDeviceCookieSeconds = 365 * 24 * 60 * 60

func (s *Server) setTrustedDeviceCookie(response http.ResponseWriter, value string, request *http.Request) {
	http.SetCookie(response, &http.Cookie{Name: service.TrustedDeviceCookieName, Value: value, Path: "/", MaxAge: trustedDeviceCookieSeconds, Expires: time.Now().Add(365 * 24 * time.Hour), HttpOnly: true, Secure: s.requestIsSecure(request), SameSite: http.SameSiteStrictMode})
}

func (s *Server) uploadAuthorization(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "private, no-store")
	if isDirectLoopbackRequest(request) {
		writeJSON(response, http.StatusOK, map[string]any{"trusted": true, "source": "local_system", "deviceName": "本机"})
		return
	}
	cookie, err := request.Cookie(service.TrustedDeviceCookieName)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{"trusted": false, "source": "invite", "deviceName": ""})
		return
	}
	auth, err := s.service.AuthorizeTrustedDevice(request.Context(), cookie.Value, s.clientIP(request))
	if err != nil {
		respondError(response, err)
		return
	}
	if auth.Device == nil {
		http.SetCookie(response, &http.Cookie{Name: service.TrustedDeviceCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.requestIsSecure(request), SameSite: http.SameSiteStrictMode})
		writeJSON(response, http.StatusOK, map[string]any{"trusted": false, "source": "invite", "deviceName": ""})
		return
	}
	if auth.Renew {
		s.setTrustedDeviceCookie(response, cookie.Value, request)
	}
	writeJSON(response, http.StatusOK, map[string]any{"trusted": true, "source": "trusted_device", "deviceName": auth.Device.Name})
}

func (s *Server) resolvePairCode(response http.ResponseWriter, request *http.Request) {
	if !s.limit(response, request, "pair_resolve_hard", 10, 10*time.Minute) {
		return
	}
	failureKey := "pair_fail:" + s.clientIP(request)
	if !isDirectLoopbackRequest(request) && s.limiter.failures(failureKey) >= 2 {
		writeError(response, http.StatusForbidden, "challenge_required", "当前本机服务未配置 Turnstile，不能开放公网配对")
		return
	}
	var input struct {
		Code           string `json:"code"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	resolvedURL, expires, err := s.service.ResolvePairCode(request.Context(), input.Code)
	if err != nil {
		s.limiter.recordFailure(failureKey, 10*time.Minute)
		writeError(response, http.StatusNotFound, "invalid_pair_code", "配对码无效或已失效")
		return
	}
	s.limiter.clear(failureKey)
	writeJSON(response, http.StatusCreated, map[string]any{"url": resolvedURL, "expiresAt": expires})
}

func (s *Server) managedPairCode(response http.ResponseWriter, request *http.Request) {
	state, err := s.service.EnsureManagedPairCode(request.Context(), request.PathValue("token"))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, state)
}
func (s *Server) disableManagedPairCode(response http.ResponseWriter, request *http.Request) {
	if err := s.service.DisableManagedPairCode(request.Context(), request.PathValue("token")); err != nil {
		respondError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func localSystemDevice() core.TrustedDevice {
	now := time.Now().UTC()
	return core.TrustedDevice{ID: "local-system", Name: "本机", System: true, Scope: "local", ExpiresAt: now.Add(100 * 365 * 24 * time.Hour), LastSeenAt: now, CreatedAt: now}
}
