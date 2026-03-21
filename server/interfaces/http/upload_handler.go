package http

import (
	"encoding/json"
	"net/http"

	"github.com/wsmbsbbz/tts-ijc/server/application"
)

// UploadHandler handles presigned upload URL requests.
type UploadHandler struct {
	svc *application.UploadService
}

// NewUploadHandler creates an UploadHandler.
func NewUploadHandler(svc *application.UploadService) *UploadHandler {
	return &UploadHandler{svc: svc}
}

// HandleRequestURL handles POST /api/upload-url.
func (h *UploadHandler) HandleRequestURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req UploadURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Filename == "" || req.ContentType == "" {
		writeError(w, http.StatusBadRequest, "filename and content_type are required")
		return
	}

	result, err := h.svc.RequestUploadURL(r.Context(), req.Filename, req.ContentType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate upload URL")
		return
	}

	writeJSON(w, http.StatusOK, UploadURLResponse{
		UploadURL: result.UploadURL,
		ObjectKey: result.ObjectKey,
	})
}
