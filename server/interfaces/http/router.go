package http

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// NewRouter creates the HTTP mux with all API routes registered.
func NewRouter(jobHandler *JobHandler, uploadHandler *UploadHandler, opts ...func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/upload-url", uploadHandler.HandleRequestURL)
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			jobHandler.HandleCreate(w, r)
		case http.MethodGet:
			jobHandler.HandleList(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/jobs/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/download") {
			jobHandler.HandleDownload(w, r)
		} else {
			jobHandler.HandleGet(w, r)
		}
	})

	// Static frontend (served when FRONTEND_DIR is set)
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		mux.Handle("/", http.FileServer(http.Dir(dir)))
	}

	// Apply middleware stack: CORS → Logging → Recovery → (optional) → mux
	var handler http.Handler = mux
	for _, mw := range opts {
		handler = mw(handler)
	}
	handler = Recovery(handler)
	handler = Logging(handler)
	handler = CORS(handler)

	return handler
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
