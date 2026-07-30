package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/core"
	"github.com/ei4869/SharePipe-OSS/internal/service"
)

func (s *Server) publicConfig(response http.ResponseWriter, request *http.Request) {
	cfg := s.service.Config()
	runtimeSettings := s.service.RuntimeSettings()
	defaultStorage, _ := s.service.DefaultStorage(request.Context())
	storageKind, _ := s.service.StorageKind(request.Context(), defaultStorage)
	writeJSON(response, http.StatusOK, map[string]any{
		"expiryPresets":        cfg.Sharing.AllowedExpiries,
		"defaultExpiry":        runtimeSettings.DefaultExpiry,
		"defaultDownloadLimit": runtimeSettings.DefaultDownloadLimit,
		"chunkSize":            cfg.Uploads.ChunkSizeBytes,
		"maxFileBytes":         runtimeSettings.MaxFileBytes,
		"maxShareBytes":        runtimeSettings.MaxShareBytes,
		"maxFilesPerShare":     runtimeSettings.MaxFilesPerShare,
		"storageBackend":       storageKind,
		"turnstileSiteKey":     "",
	})
}

func (s *Server) validateInvite(response http.ResponseWriter, request *http.Request) {
	if !s.limit(response, request, "invite_validate", 20, 5*time.Minute) {
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	if err := s.service.ValidateInvite(request.Context(), input.Code); err != nil {
		s.service.Audit(request.Context(), "", "invite.validate", "denied", s.clientIP(request), request.UserAgent(), "")
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"valid": true})
}

func (s *Server) createShare(response http.ResponseWriter, request *http.Request) {
	if !s.limit(response, request, "share_create", 20, 10*time.Minute) {
		return
	}
	var input struct {
		InviteCode     string               `json:"inviteCode"`
		Name           string               `json:"name"`
		Description    string               `json:"description"`
		ExpiresIn      string               `json:"expiresIn"`
		DownloadLimit  *int                 `json:"downloadLimit"`
		Password       string               `json:"password"`
		Entries        []core.ManifestEntry `json:"entries"`
		PairingEnabled *bool                `json:"pairingEnabled"`
		QREnabled      *bool                `json:"qrEnabled"`
	}
	if !decodeJSON(response, request, 2*1024*1024, &input) {
		return
	}
	local := isDirectLoopbackRequest(request)
	trustedID := ""
	if !local {
		if cookie, cookieErr := request.Cookie(service.TrustedDeviceCookieName); cookieErr == nil {
			authorization, authErr := s.service.AuthorizeTrustedDevice(request.Context(), cookie.Value, s.clientIP(request))
			if authErr != nil {
				respondError(response, authErr)
				return
			}
			if authorization.Device != nil {
				trustedID = authorization.Device.ID
				if authorization.Renew {
					s.setTrustedDeviceCookie(response, cookie.Value, request)
				}
			}
		}
	}
	pairingEnabled := true
	if input.PairingEnabled != nil {
		pairingEnabled = *input.PairingEnabled
	}
	qrEnabled := true
	if input.QREnabled != nil {
		qrEnabled = *input.QREnabled
	}
	creation, err := s.service.CreateShare(request.Context(), service.CreateShareInput{
		InviteCode: input.InviteCode, Name: input.Name, Description: input.Description,
		ExpiresIn: input.ExpiresIn, DownloadLimit: input.DownloadLimit,
		Password: input.Password, Entries: input.Entries, TrustedDeviceID: trustedID, LocalSystem: local,
		PairingEnabled: pairingEnabled, QREnabled: qrEnabled, RemoteIP: s.clientIP(request),
		UserAgent: request.UserAgent(), Platform: request.Header.Get("Sec-CH-UA-Platform"),
	})
	if err != nil {
		s.service.Audit(request.Context(), "", "share.create", "denied", s.clientIP(request), request.UserAgent(), "")
		respondError(response, err)
		return
	}
	if creation.TrustedDeviceToken != "" {
		s.setTrustedDeviceCookie(response, creation.TrustedDeviceToken, request)
	}
	s.service.Audit(request.Context(), creation.Share.ID, "share.create", "ok", s.clientIP(request), request.UserAgent(), "")
	writeJSON(response, http.StatusCreated, creation)
}

func (s *Server) recoverUpload(response http.ResponseWriter, request *http.Request) {
	if !s.limit(response, request, "upload_recovery", 60, 10*time.Minute) {
		return
	}
	creation, err := s.service.RecoverUpload(request.Context(), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, creation)
}

func (s *Server) uploadProgress(response http.ResponseWriter, request *http.Request) {
	progress, err := s.service.UploadProgress(request.Context(), request.PathValue("sessionID"), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

func (s *Server) putUploadPart(response http.ResponseWriter, request *http.Request) {
	partNumber, err := strconv.Atoi(request.PathValue("partNumber"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_part", "分片编号无效")
		return
	}
	cfg := s.service.Config()
	request.Body = http.MaxBytesReader(response, request.Body, cfg.Uploads.ChunkSizeBytes+1)
	progress, err := s.service.PutLocalPart(
		request.Context(), request.PathValue("sessionID"), bearerToken(request), partNumber, request.Body,
	)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

func (s *Server) presignUploadPart(response http.ResponseWriter, request *http.Request) {
	partNumber, err := strconv.Atoi(request.PathValue("partNumber"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_part", "分片编号无效")
		return
	}
	url, err := s.service.PresignS3Part(request.Context(), request.PathValue("sessionID"), bearerToken(request), partNumber)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) acknowledgeS3Part(response http.ResponseWriter, request *http.Request) {
	partNumber, err := strconv.Atoi(request.PathValue("partNumber"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_part", "分片编号无效")
		return
	}
	var input struct {
		ETag   string `json:"etag"`
		SHA256 string `json:"sha256"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	part := core.UploadPart{PartNumber: partNumber, ETag: input.ETag, SHA256: input.SHA256}
	progress, err := s.service.AcknowledgeS3Part(request.Context(), request.PathValue("sessionID"), bearerToken(request), part)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

func (s *Server) completeUpload(response http.ResponseWriter, request *http.Request) {
	var input service.CompleteUploadInput
	if !decodeJSON(response, request, 2*1024*1024, &input) {
		return
	}
	progress, err := s.service.CompleteUpload(
		request.Context(), request.PathValue("sessionID"), bearerToken(request), input,
	)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

func (s *Server) publicShare(response http.ResponseWriter, request *http.Request) {
	public, err := s.publicShareForRequest(request)
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, public)
}

func (s *Server) publicShareForRequest(request *http.Request) (core.PublicShare, error) {
	token := request.PathValue("token")
	public, err := s.service.GetPublicShare(request.Context(), token, false)
	if err != nil {
		return core.PublicShare{}, err
	}
	if public.Locked && s.shareUnlocked(request, public.Share.ID) {
		return s.service.GetPublicShare(request.Context(), token, true)
	}
	return public, nil
}

func (s *Server) unlockShare(response http.ResponseWriter, request *http.Request) {
	if !s.limit(response, request, "share_unlock", 10, 5*time.Minute) {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	share, err := s.service.VerifySharePassword(request.Context(), request.PathValue("token"), input.Password)
	if err != nil {
		s.service.Audit(request.Context(), "", "share.unlock", "denied", s.clientIP(request), request.UserAgent(), "")
		respondError(response, err)
		return
	}
	passwordVersion, err := s.service.SharePasswordVersion(request.Context(), share.ID)
	if err != nil {
		respondError(response, err)
		return
	}
	if err := s.setShareCookie(response, share.ID, passwordVersion, share.ExpiresAt); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), share.ID, "share.unlock", "ok", s.clientIP(request), request.UserAgent(), "")
	writeJSON(response, http.StatusOK, map[string]bool{"unlocked": true})
}

func (s *Server) previewEntry(response http.ResponseWriter, request *http.Request) {
	public, err := s.publicShareForRequest(request)
	if err != nil {
		respondError(response, err)
		return
	}
	resource, err := s.service.ResolvePreview(
		request.Context(), request.PathValue("token"), !public.Locked, request.PathValue("entryID"),
	)
	if err != nil {
		respondError(response, err)
		return
	}
	s.serveResource(response, request, resource, true)
}

func (s *Server) createDownload(response http.ResponseWriter, request *http.Request) {
	public, err := s.publicShareForRequest(request)
	if err != nil {
		respondError(response, err)
		return
	}
	var input struct {
		EntryID string `json:"entryId"`
		Kind    string `json:"kind"`
	}
	if !decodeJSON(response, request, 64*1024, &input) {
		return
	}
	grant, err := s.service.CreateDownloadGrant(
		request.Context(), request.PathValue("token"), !public.Locked, input.EntryID, input.Kind,
	)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), public.Share.ID, "download."+input.Kind, "granted", s.clientIP(request), request.UserAgent(), "")
	status := http.StatusCreated
	if grant.Status == "preparing" {
		status = http.StatusAccepted
	}
	writeJSON(response, status, grant)
}

func (s *Server) downloadStatus(response http.ResponseWriter, request *http.Request) {
	grant, err := s.service.GetDownloadGrant(request.Context(), request.PathValue("token"))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, grant)
}
