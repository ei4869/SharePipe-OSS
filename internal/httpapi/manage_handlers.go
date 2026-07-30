package httpapi

import (
	"net/http"

	"github.com/ei4869/SharePipe-OSS/internal/service"
)

func (s *Server) managedShare(response http.ResponseWriter, request *http.Request) {
	share, err := s.service.GetManagedShare(request.Context(), request.PathValue("token"))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, share)
}

func (s *Server) updateManagedShare(response http.ResponseWriter, request *http.Request) {
	var input service.UpdateShareInput
	if !decodeJSON(response, request, 128*1024, &input) {
		return
	}
	share, err := s.service.UpdateManagedShare(request.Context(), request.PathValue("token"), input)
	if err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), share.Share.ID, "share.update", "ok", s.clientIP(request), request.UserAgent(), "")
	writeJSON(response, http.StatusOK, share)
}

func (s *Server) revokeManagedShare(response http.ResponseWriter, request *http.Request) {
	share, err := s.service.GetManagedShare(request.Context(), request.PathValue("token"))
	if err != nil {
		respondError(response, err)
		return
	}
	if err := s.service.RevokeManagedShare(request.Context(), request.PathValue("token")); err != nil {
		respondError(response, err)
		return
	}
	s.service.Audit(request.Context(), share.Share.ID, "share.revoke", "ok", s.clientIP(request), request.UserAgent(), "")
	response.WriteHeader(http.StatusNoContent)
}
