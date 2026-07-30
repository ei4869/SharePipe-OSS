package httpapi

import (
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ei4869/SharePipe-OSS/internal/security"
	"github.com/ei4869/SharePipe-OSS/internal/service"
)

type Server struct {
	service         *service.Service
	static          fs.FS
	secure          bool
	publicHost      string
	adminAccess     string
	csp             string
	trusted         []*net.IPNet
	limiter         *rateLimiter
	accessValidator *security.AccessValidator
}

func New(svc *service.Service, static fs.FS) http.Handler {
	base, _ := url.Parse(svc.Config().Server.PublicBaseURL)
	server := &Server{
		service: svc, static: static, secure: base.Scheme == "https",
		publicHost:  base.Scheme + "://" + base.Host,
		adminAccess: svc.Config().Server.AdminAccess,
		csp:         contentSecurityPolicy(svc.Config().Storage.S3.Endpoint),
		trusted:     trustedProxyNetworks(svc.Config().Server.TrustedProxyCIDRs),
		limiter:     newRateLimiter(),
	}
	if server.adminAccess == "cloudflare-access" {
		server.accessValidator, _ = security.NewAccessValidator(
			svc.Config().Server.CloudflareAccess.TeamDomain,
			svc.Config().Server.CloudflareAccess.Audience,
			nil,
		)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", server.health)
	mux.HandleFunc("GET /api/v1/public/config", server.publicConfig)
	mux.HandleFunc("POST /api/v1/invites/validate", server.validateInvite)
	mux.HandleFunc("POST /api/v1/shares", server.createShare)
	mux.HandleFunc("GET /api/v1/upload-recovery", server.recoverUpload)
	mux.HandleFunc("GET /api/v1/upload-tasks/{shareID}", server.uploadTask)
	mux.HandleFunc("POST /api/v1/upload-tasks/{shareID}/pause", server.pauseUploadTask)
	mux.HandleFunc("POST /api/v1/upload-tasks/{shareID}/resume", server.resumeUploadTask)
	mux.HandleFunc("DELETE /api/v1/upload-tasks/{shareID}", server.cancelUploadTask)
	mux.HandleFunc("GET /api/v1/upload-authorization", server.uploadAuthorization)
	mux.HandleFunc("POST /api/v1/pairing/resolve", server.resolvePairCode)
	mux.HandleFunc("GET /api/v1/uploads/{sessionID}", server.uploadProgress)
	mux.HandleFunc("PUT /api/v1/uploads/{sessionID}/parts/{partNumber}", server.putUploadPart)
	mux.HandleFunc("POST /api/v1/uploads/{sessionID}/s3-parts/{partNumber}", server.presignUploadPart)
	mux.HandleFunc("POST /api/v1/uploads/{sessionID}/s3-parts/{partNumber}/ack", server.acknowledgeS3Part)
	mux.HandleFunc("POST /api/v1/uploads/{sessionID}/complete", server.completeUpload)

	mux.HandleFunc("GET /api/v1/public/shares/{token}", server.publicShare)
	mux.HandleFunc("POST /api/v1/public/shares/{token}/unlock", server.unlockShare)
	mux.HandleFunc("GET /api/v1/public/shares/{token}/entries/{entryID}/preview", server.previewEntry)
	mux.HandleFunc("POST /api/v1/public/shares/{token}/downloads", server.createDownload)
	mux.HandleFunc("GET /api/v1/downloads/{token}", server.download)
	mux.HandleFunc("GET /api/v1/downloads/{token}/status", server.downloadStatus)

	mux.HandleFunc("GET /api/v1/manage/{token}", server.managedShare)
	mux.HandleFunc("PATCH /api/v1/manage/{token}", server.updateManagedShare)
	mux.HandleFunc("DELETE /api/v1/manage/{token}", server.revokeManagedShare)
	mux.HandleFunc("POST /api/v1/manage/{token}/pair-code", server.managedPairCode)
	mux.HandleFunc("DELETE /api/v1/manage/{token}/pair-code", server.disableManagedPairCode)

	mux.Handle("POST /api/v1/admin/login", server.adminSurface(http.HandlerFunc(server.adminLogin)))
	mux.Handle("GET /api/v1/admin/session", server.adminSurface(http.HandlerFunc(server.adminSession)))
	mux.Handle("POST /api/v1/admin/logout", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminLogout))))
	mux.Handle("GET /api/v1/admin/overview", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminOverview))))
	mux.Handle("GET /api/v1/admin/shares", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminShares))))
	mux.Handle("DELETE /api/v1/admin/shares/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminRevokeShare))))
	mux.Handle("GET /api/v1/admin/invites", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminInvites))))
	mux.Handle("POST /api/v1/admin/invites", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminCreateInvite))))
	mux.Handle("DELETE /api/v1/admin/invites/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminRevokeInvite))))
	mux.Handle("GET /api/v1/admin/trusted-devices", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminTrustedDevices))))
	mux.Handle("PATCH /api/v1/admin/trusted-devices/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminRenameTrustedDevice))))
	mux.Handle("DELETE /api/v1/admin/trusted-devices/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminRevokeTrustedDevice))))
	mux.Handle("POST /api/v1/admin/trusted-devices/bootstrap", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminTrustedDeviceBootstrap))))
	mux.Handle("GET /api/v1/admin/settings", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminSettings))))
	mux.Handle("PUT /api/v1/admin/settings", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminUpdateSettings))))
	mux.Handle("PUT /api/v1/admin/settings/default-storage", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminDefaultStorage))))
	mux.Handle("POST /api/v1/admin/diagnostics", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminDiagnostics))))
	mux.Handle("POST /api/v1/admin/maintenance", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminMaintenance))))
	mux.Handle("GET /api/v1/admin/security", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminSecurity))))
	mux.Handle("POST /api/v1/admin/security/emails", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminAddEmail))))
	mux.Handle("DELETE /api/v1/admin/security/emails/{email}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminRemoveEmail))))
	mux.Handle("PUT /api/v1/admin/security/password", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminChangePassword))))
	mux.Handle("GET /api/v1/admin/storage/providers", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminStorageProviders))))
	mux.Handle("GET /api/v1/admin/storage/profiles", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminStorageProfiles))))
	mux.Handle("POST /api/v1/admin/storage/profiles", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminCreateStorageProfile))))
	mux.Handle("PUT /api/v1/admin/storage/profiles/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminUpdateStorageProfile))))
	mux.Handle("DELETE /api/v1/admin/storage/profiles/{id}", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminDeleteStorageProfile))))
	mux.Handle("POST /api/v1/admin/storage/profiles/{id}/test", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminTestStorageProfile))))
	mux.Handle("POST /api/v1/admin/storage/test", server.adminSurface(server.requireAdmin(http.HandlerFunc(server.adminTestStorageInput))))

	mux.HandleFunc("/", server.serveSPA)
	return server.securityHeaders(server.httpsRedirect(server.sameOrigin(mux)))
}

func (s *Server) serveSPA(response http.ResponseWriter, request *http.Request) {
	if isAdminPath(request.URL.Path) && !s.adminRequestAllowed(request) {
		http.NotFound(response, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/") {
		writeError(response, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	if s.static == nil {
		writeError(response, http.StatusServiceUnavailable, "frontend_unavailable", "前端资源尚未构建")
		return
	}
	cleaned := strings.TrimPrefix(request.URL.Path, "/")
	if cleaned == "" {
		cleaned = "index.html"
	}
	if file, err := fs.Stat(s.static, cleaned); err == nil && !file.IsDir() {
		http.FileServer(http.FS(s.static)).ServeHTTP(response, request)
		return
	}
	index, err := fs.ReadFile(s.static, "index.html")
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "frontend_unavailable", "前端资源尚未构建")
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-cache")
	_, _ = response.Write(index)
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "ok", "service": "SharePipe", "time": time.Now().UTC(),
	})
}
