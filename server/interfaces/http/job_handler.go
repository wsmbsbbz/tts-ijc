package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const downloadURLExpiry = 5 * time.Minute

// JobHandler handles job-related HTTP requests.
type JobHandler struct {
	jobSvc  *application.JobService
	storage domain.FileStorage
}

// NewJobHandler creates a JobHandler.
func NewJobHandler(jobSvc *application.JobService, storage domain.FileStorage) *JobHandler {
	return &JobHandler{jobSvc: jobSvc, storage: storage}
}

// HandleCreate handles POST /api/jobs.
func (h *JobHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AudioKey == "" || req.VTTKey == "" {
		writeError(w, http.StatusBadRequest, "audio_key and vtt_key are required")
		return
	}

	user, _ := UserFromContext(r.Context())
	job, err := h.jobSvc.CreateJob(r.Context(), user.ID, req.AudioKey, req.VTTKey, req.AudioName, req.VTTName, req.ToJobConfig())
	if err != nil {
		if errors.Is(err, domain.ErrQueueFull) {
			writeError(w, http.StatusServiceUnavailable, "server is busy, please try again later")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	writeJSON(w, http.StatusCreated, JobFromDomain(job))
}

// HandleGet handles GET /api/jobs/{id}.
func (h *JobHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := extractJobID(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}

	job, err := h.jobSvc.GetJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	// Prevent users from accessing each other's jobs.
	user, _ := UserFromContext(r.Context())
	if job.UserID != "" && job.UserID != user.ID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, JobFromDomain(job))
}

// HandleList handles GET /api/jobs.
func (h *JobHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, _ := UserFromContext(r.Context())
	jobs, err := h.jobSvc.ListJobs(r.Context(), user.ID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	responses := make([]JobResponse, len(jobs))
	for i, j := range jobs {
		responses[i] = JobFromDomain(j)
	}

	writeJSON(w, http.StatusOK, responses)
}

// HandleDownload handles GET /api/jobs/{id}/download.
// Generates a short-lived presigned URL and redirects the browser to it.
// The presigned URL includes Content-Disposition so the browser saves the file
// using the original audio filename.
func (h *JobHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	id := extractDownloadJobID(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}

	job, err := h.jobSvc.GetJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	// Prevent users from downloading each other's jobs.
	downloadUser, _ := UserFromContext(r.Context())
	if job.UserID != "" && job.UserID != downloadUser.ID {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	if job.Status != domain.StatusCompleted || job.OutputKey == "" {
		writeError(w, http.StatusConflict, "job output not ready")
		return
	}

	downloadURL, err := h.storage.GenerateDownloadURL(r.Context(), job.OutputKey, downloadURLExpiry, job.AudioName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate download URL")
		return
	}

	http.Redirect(w, r, downloadURL, http.StatusFound)
}

// extractJobID extracts the job ID from a path like /api/jobs/{id}.
func extractJobID(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	// Exclude /api/jobs/{id}/download
	if len(parts) >= 5 {
		return ""
	}
	return parts[3]
}

// extractDownloadJobID extracts the job ID from /api/jobs/{id}/download.
func extractDownloadJobID(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 5 || parts[4] != "download" {
		return ""
	}
	return parts[3]
}
