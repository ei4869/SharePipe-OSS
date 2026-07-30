package httpapi

import "net/http"

func (s *Server) uploadTask(response http.ResponseWriter, request *http.Request) {
	task, err := s.service.UploadTask(request.Context(), request.PathValue("shareID"), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, task)
}

func (s *Server) pauseUploadTask(response http.ResponseWriter, request *http.Request) {
	task, err := s.service.PauseUploadTask(request.Context(), request.PathValue("shareID"), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, task)
}

func (s *Server) resumeUploadTask(response http.ResponseWriter, request *http.Request) {
	task, err := s.service.ResumeUploadTask(request.Context(), request.PathValue("shareID"), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, task)
}

func (s *Server) cancelUploadTask(response http.ResponseWriter, request *http.Request) {
	task, err := s.service.CancelUploadTask(request.Context(), request.PathValue("shareID"), bearerToken(request))
	if err != nil {
		respondError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, task)
}
