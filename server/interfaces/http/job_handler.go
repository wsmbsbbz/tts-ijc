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

const downloadURLExpiry = 1 * time.Hour

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

	job, err := h.jobSvc.CreateJob(r.Context(), req.AudioKey, req.VTTKey, req.ToJobConfig())
	if err != nil {
		if errors.Is(err, domain.ErrQueueFull) {
			writeError(w, http.StatusServiceUnavailable, "server is busy, please try again later")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	writeJSON(w, http.StatusCreated, JobFromDomain(job, ""))
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

	var downloadURL string
	if job.Status == domain.StatusCompleted && job.OutputKey != "" {
		downloadURL, _ = h.storage.GenerateDownloadURL(r.Context(), job.OutputKey, downloadURLExpiry)
	}

	writeJSON(w, http.StatusOK, JobFromDomain(job, downloadURL))
}

// HandleList handles GET /api/jobs.
func (h *JobHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobs, err := h.jobSvc.ListJobs(r.Context(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	responses := make([]JobResponse, len(jobs))
	for i, j := range jobs {
		var downloadURL string
		if j.Status == domain.StatusCompleted && j.OutputKey != "" {
			downloadURL, _ = h.storage.GenerateDownloadURL(r.Context(), j.OutputKey, downloadURLExpiry)
		}
		responses[i] = JobFromDomain(j, downloadURL)
	}

	writeJSON(w, http.StatusOK, responses)
}

// extractJobID extracts the job ID from a path like /api/jobs/{id}.
func extractJobID(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}
