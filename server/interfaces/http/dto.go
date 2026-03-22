package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// --- Request DTOs ---

// UploadURLRequest is the JSON body for POST /api/upload-url.
type UploadURLRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// CreateJobRequest is the JSON body for POST /api/jobs.
type CreateJobRequest struct {
	AudioKey    string  `json:"audio_key"`
	VTTKey      string  `json:"vtt_key"`
	AudioName   string  `json:"audio_name"`
	VTTName     string  `json:"vtt_name"`
	TTSProvider string  `json:"tts_provider"`
	TTSVolume   float64 `json:"tts_volume"`
	NoSpeedup   bool    `json:"no_speedup"`
	Concurrency int     `json:"concurrency"`
}

// ToJobConfig converts the request DTO to a domain value object.
func (r CreateJobRequest) ToJobConfig() domain.JobConfig {
	return domain.JobConfig{
		TTSProvider: r.TTSProvider,
		TTSVolume:   r.TTSVolume,
		NoSpeedup:   r.NoSpeedup,
		Concurrency: r.Concurrency,
	}
}

// --- Response DTOs ---

// UploadURLResponse is returned by POST /api/upload-url.
type UploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
}

// JobResponse is the JSON representation of a job.
// DownloadURL is intentionally omitted; use GET /api/jobs/{id}/download instead.
type JobResponse struct {
	JobID       string  `json:"job_id"`
	Status      string  `json:"status"`
	Progress    string  `json:"progress"`
	AudioName   string  `json:"audio_name"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
	Error       *string `json:"error"`
}

// JobFromDomain converts a domain.Job to a response DTO.
func JobFromDomain(j domain.Job) JobResponse {
	resp := JobResponse{
		JobID:     j.ID,
		Status:    string(j.Status),
		Progress:  j.Progress,
		AudioName: j.AudioName,
		CreatedAt: j.CreatedAt.Format(time.RFC3339),
		Error:     j.Error,
	}
	if j.CompletedAt != nil {
		s := j.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &s
	}
	return resp
}

// ErrorResponse is the JSON body for error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// --- Auth DTOs ---

// RegisterRequest is the JSON body for POST /api/auth/register.
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginRequest is the JSON body for POST /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse is returned on successful register/login.
type AuthResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// --- Helpers ---

// extractBearerToken reads the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(v, "Bearer ")
}
