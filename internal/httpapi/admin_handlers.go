package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/service"
)

func (s *Server) adminLogin(response http.ResponseWriter, request *http.Request) {
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	if identity.Local {
		writeJSON(response, http.StatusOK, map[string]bool{"authenticated": true})
		return
	}
	if !s.limit(response, request, "admin_login", 5, 5*time.Minute) {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	if !s.service.AuthenticateAdmin(input.Password) {
		s.service.Audit(request.Context(), "", "admin.login", "denied", s.clientIP(request), request.UserAgent(), "")
		writeError(response, http.StatusUnauthorized, "invalid_password", "管理员密码不正确")
		return
	}
	version := s.service.AdminPasswordVersion(request.Context())
	token, err := s.service.Signer().Sign(identity.Email, "admin:"+strconv.Itoa(version), s.service.AdministratorSessionTTL())
	if err != nil {
		respondError(response, err)
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: "sharepipe_admin", Value: token, Path: "/api/v1/admin", MaxAge: 12 * 60 * 60,
		Expires: time.Now().Add(12 * time.Hour), HttpOnly: true, Secure: s.requestIsSecure(request), SameSite: http.SameSiteStrictMode,
	})
	s.service.Audit(request.Context(), "", "admin.login", "ok", s.clientIP(request), request.UserAgent(), "")
	writeJSON(response, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) adminSession(response http.ResponseWriter, request *http.Request) {
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	authenticated := identity.Local
	if !identity.Local {
		if cookie, err := request.Cookie("sharepipe_admin"); err == nil {
			scope := "admin:" + strconv.Itoa(s.service.AdminPasswordVersion(request.Context()))
			if claims, verifyErr := s.service.Signer().Verify(cookie.Value, scope); verifyErr == nil && claims.Subject == identity.Email {
				authenticated = true
			}
		}
	}
	securitySettings, err := s.service.AdministratorSecurity(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"local": identity.Local, "email": identity.Email, "authenticated": authenticated,
		"passwordConfigured": securitySettings.PasswordConfigured,
	})
}

func (s *Server) adminLogout(response http.ResponseWriter, request *http.Request) {
	http.SetCookie(response, &http.Cookie{
		Name: "sharepipe_admin", Value: "", Path: "/api/v1/admin", MaxAge: -1,
		HttpOnly: true, Secure: s.requestIsSecure(request), SameSite: http.SameSiteStrictMode,
	})
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminOverview(response http.ResponseWriter, request *http.Request) {
	overview, err := s.service.AdminOverview(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, overview)
}

func (s *Server) adminShares(response http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
	shares, err := s.service.ListShares(request.Context(), limit, offset)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": shares, "limit": limit, "offset": offset})
}

func (s *Server) adminRevokeShare(response http.ResponseWriter, request *http.Request) {
	shareID := request.PathValue("id")
	if err := s.service.RevokeShareByID(request.Context(), shareID); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), shareID, "share.admin_revoke", "ok", s.clientIP(request), request.UserAgent(), "")
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminInvites(response http.ResponseWriter, request *http.Request) {
	invites, err := s.service.ListInvites(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": invites})
}

func (s *Server) adminCreateInvite(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Label      string `json:"label"`
		MaxUses    *int   `json:"maxUses"`
		ExpiresIn  string `json:"expiresIn"`
		Kind       string `json:"kind"`
		DeviceName string `json:"deviceName"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	var expiresAt *time.Time
	if input.ExpiresIn != "" {
		duration, err := time.ParseDuration(input.ExpiresIn)
		if err != nil || duration <= 0 || duration > 365*24*time.Hour {
			writeError(response, http.StatusBadRequest, "invalid_expiry", "邀请码有效期无效")
			return
		}
		value := time.Now().Add(duration).UTC()
		expiresAt = &value
	}
	invite, err := s.service.CreateInviteWithKind(request.Context(), input.Label, input.MaxUses, expiresAt, input.Kind, input.DeviceName)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "invite.create", "ok", s.clientIP(request), request.UserAgent(), invite.ID)
	writeJSON(response, http.StatusCreated, invite)
}

func (s *Server) adminTrustedDevices(response http.ResponseWriter, request *http.Request) {
	scope := request.URL.Query().Get("scope")
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	if scope == "cloud" {
		if !identity.Local || !s.service.CloudAdministratorControlConfigured() {
			writeError(response, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		items, err := s.service.CloudTrustedDevices(request.Context())
		if err != nil {
			respondError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items, "scope": "cloud"})
		return
	}
	items, err := s.service.ListTrustedDevices(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	items = append([]core.TrustedDevice{localSystemDevice()}, items...)
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "scope": "local", "cloudAvailable": identity.Local && s.service.CloudAdministratorControlConfigured()})
}

func (s *Server) adminRenameTrustedDevice(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	if request.URL.Query().Get("scope") == "cloud" {
		item, err := s.service.CloudRenameTrustedDevice(request.Context(), request.PathValue("id"), input.Name)
		if err != nil {
			respondError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, item)
		return
	}
	if request.PathValue("id") == "local-system" {
		writeError(response, http.StatusConflict, "system_device", "本机系统设备不能修改")
		return
	}
	item, err := s.service.RenameTrustedDevice(request.Context(), request.PathValue("id"), input.Name)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (s *Server) adminRevokeTrustedDevice(response http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("scope") == "cloud" {
		if err := s.service.CloudRevokeTrustedDevice(request.Context(), request.PathValue("id")); err != nil {
			respondError(response, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if request.PathValue("id") == "local-system" {
		writeError(response, http.StatusConflict, "system_device", "本机系统设备不能移除")
		return
	}
	if err := s.service.RevokeTrustedDevice(request.Context(), request.PathValue("id")); err != nil {
		respondError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminTrustedDeviceBootstrap(response http.ResponseWriter, request *http.Request) {
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	if !identity.Local {
		http.NotFound(response, request)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	link, expires, err := s.service.CloudTrustedDeviceBootstrap(request.Context(), input.Name)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"url": link, "expiresAt": expires})
}

func (s *Server) adminRevokeInvite(response http.ResponseWriter, request *http.Request) {
	if err := s.service.RevokeInvite(request.Context(), request.PathValue("id")); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "invite.revoke", "ok", s.clientIP(request), request.UserAgent(), request.PathValue("id"))
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminDefaultStorage(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Backend string `json:"backend"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	if err := s.service.SetDefaultStorage(request.Context(), input.Backend); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "storage.default", "ok", s.clientIP(request), request.UserAgent(), input.Backend)
	writeJSON(response, http.StatusOK, map[string]string{"backend": input.Backend})
}

func (s *Server) adminSettings(response http.ResponseWriter, request *http.Request) {
	defaultStorage, err := s.service.DefaultStorage(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"limits": s.service.RuntimeSettings(), "defaultStorage": defaultStorage,
	})
}

func (s *Server) adminUpdateSettings(response http.ResponseWriter, request *http.Request) {
	var input service.UpdateRuntimeSettings
	if !decodeJSON(response, request, 128*1024, &input) {
		return
	}
	settings, err := s.service.UpdateSettings(request.Context(), input)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "settings.update", "ok", s.clientIP(request), request.UserAgent(), "")
	writeJSON(response, http.StatusOK, settings)
}

func (s *Server) adminDiagnostics(response http.ResponseWriter, request *http.Request) {
	until, err := s.service.EnableDiagnosticMode(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"enabled": true, "until": until})
}

func (s *Server) adminMaintenance(response http.ResponseWriter, request *http.Request) {
	if err := s.service.RunMaintenanceOnce(request.Context()); err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"completed": true})
}

func (s *Server) adminSecurity(response http.ResponseWriter, request *http.Request) {
	if !s.requireLocalSecurityControl(response, request) {
		return
	}
	settings, err := s.administratorSecurity(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	writeJSON(response, http.StatusOK, map[string]any{
		"emails": settings.Emails, "passwordConfigured": settings.PasswordConfigured,
		"local": identity.Local, "currentEmail": identity.Email,
	})
}

func (s *Server) adminAddEmail(response http.ResponseWriter, request *http.Request) {
	if !s.requireLocalSecurityControl(response, request) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	email, err := s.addAdministratorEmail(request.Context(), input.Email)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "admin.email_add", "ok", s.clientIP(request), request.UserAgent(), email)
	writeJSON(response, http.StatusCreated, map[string]string{"email": email})
}

func (s *Server) adminRemoveEmail(response http.ResponseWriter, request *http.Request) {
	if !s.requireLocalSecurityControl(response, request) {
		return
	}
	email := request.PathValue("email")
	if err := s.removeAdministratorEmail(request.Context(), email); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "admin.email_remove", "ok", s.clientIP(request), request.UserAgent(), email)
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminChangePassword(response http.ResponseWriter, request *http.Request) {
	if !s.requireLocalSecurityControl(response, request) {
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	if err := s.changeAdministratorPassword(request.Context(), input.NewPassword); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "admin.password_change", "ok", s.clientIP(request), request.UserAgent(), identity.Email)
	writeJSON(response, http.StatusOK, map[string]bool{"changed": true})
}

func (s *Server) requireLocalSecurityControl(response http.ResponseWriter, request *http.Request) bool {
	identity, _ := request.Context().Value(adminIdentityKey).(adminIdentity)
	if !identity.Local {
		http.NotFound(response, request)
		return false
	}
	return true
}

func (s *Server) administratorSecurity(ctx context.Context) (service.AdministratorSecurity, error) {
	if s.service.CloudAdministratorControlConfigured() {
		return s.service.CloudAdministratorSecurity(ctx)
	}
	return s.service.AdministratorSecurity(ctx)
}

func (s *Server) addAdministratorEmail(ctx context.Context, email string) (string, error) {
	if s.service.CloudAdministratorControlConfigured() {
		return s.service.CloudAddAdministratorEmail(ctx, email)
	}
	return s.service.AddAdminEmail(ctx, email)
}

func (s *Server) removeAdministratorEmail(ctx context.Context, email string) error {
	if s.service.CloudAdministratorControlConfigured() {
		return s.service.CloudRemoveAdministratorEmail(ctx, email)
	}
	return s.service.RemoveAdminEmail(ctx, email)
}

func (s *Server) changeAdministratorPassword(ctx context.Context, password string) error {
	if s.service.CloudAdministratorControlConfigured() {
		return s.service.CloudChangeAdministratorPassword(ctx, password)
	}
	return s.service.ChangeAdminPassword(ctx, "", password, false)
}

func (s *Server) adminStorageProviders(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"items": service.StorageProviderCatalog()})
}

func (s *Server) adminStorageProfiles(response http.ResponseWriter, request *http.Request) {
	profiles, err := s.service.ListStorageProfiles(request.Context())
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": profiles})
}

func (s *Server) adminCreateStorageProfile(response http.ResponseWriter, request *http.Request) {
	var input service.StorageProfileInput
	if !decodeJSON(response, request, 256*1024, &input) {
		return
	}
	profile, err := s.service.CreateStorageProfile(request.Context(), input)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "storage.create", "ok", s.clientIP(request), request.UserAgent(), profile.ID)
	writeJSON(response, http.StatusCreated, profile)
}

func (s *Server) adminUpdateStorageProfile(response http.ResponseWriter, request *http.Request) {
	var input service.StorageProfileInput
	if !decodeJSON(response, request, 256*1024, &input) {
		return
	}
	profile, err := s.service.UpdateStorageProfile(request.Context(), request.PathValue("id"), input)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "storage.update", "ok", s.clientIP(request), request.UserAgent(), profile.ID)
	writeJSON(response, http.StatusOK, profile)
}

func (s *Server) adminDeleteStorageProfile(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if err := s.service.DeleteStorageProfile(request.Context(), id); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), "", "storage.delete", "ok", s.clientIP(request), request.UserAgent(), id)
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminTestStorageProfile(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	health := s.service.TestStorageProfile(ctx, request.PathValue("id"))
	writeJSON(response, http.StatusOK, health)
}

func (s *Server) adminTestStorageInput(response http.ResponseWriter, request *http.Request) {
	var input service.StorageProfileInput
	if !decodeJSON(response, request, 256*1024, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	health := s.service.TestStorageInput(ctx, input)
	writeJSON(response, http.StatusOK, health)
}
