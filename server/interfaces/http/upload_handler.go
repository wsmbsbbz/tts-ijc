package http

import (
	"encoding/json"
	"net/http"

	"github.com/wsmbsbbz/tts-ijc/server/application"
)

// allowedContentTypes is the whitelist for presigned upload URLs.
// Only audio files and VTT subtitles are accepted.
var allowedContentTypes = map[string]bool{
	"audio/mpeg":  true,
	"audio/mp4":   true,
	"audio/wav":   true,
	"audio/ogg":   true,
	"audio/webm":  true,
	"audio/flac":  true,
	"text/vtt":    true,
	"text/plain":  true,
}

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

	if !allowedContentTypes[req.ContentType] {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported content type")
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
