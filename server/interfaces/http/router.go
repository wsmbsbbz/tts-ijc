package http

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// NewRouter creates the HTTP mux with all API routes registered.
func NewRouter(
	jobHandler *JobHandler,
	uploadHandler *UploadHandler,
	authHandler *AuthHandler,
	sessionAuth func(http.Handler) http.Handler,
) http.Handler {
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("/api/auth/register", authHandler.HandleRegister)
	mux.HandleFunc("/api/auth/login", authHandler.HandleLogin)
	mux.HandleFunc("/api/auth/logout", authHandler.HandleLogout)
	mux.HandleFunc("/api/tts-providers", jobHandler.HandleListProviders)
	mux.Handle("/api/me", sessionAuth(http.HandlerFunc(authHandler.HandleMe)))

	// Protected API routes — wrapped individually with sessionAuth.
	mux.Handle("/api/upload-url", sessionAuth(http.HandlerFunc(uploadHandler.HandleRequestURL)))
	mux.Handle("/api/jobs", sessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			jobHandler.HandleCreate(w, r)
		case http.MethodGet:
			jobHandler.HandleList(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})))
	mux.Handle("/api/jobs/", sessionAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/download") {
			jobHandler.HandleDownload(w, r)
		} else {
			jobHandler.HandleGet(w, r)
		}
	})))

	// Static frontend (served when FRONTEND_DIR is set)
	if dir := os.Getenv("FRONTEND_DIR"); dir != "" {
		mux.Handle("/", http.FileServer(http.Dir(dir)))
	}

	// Apply middleware stack: CORS → Logging → Recovery → mux
	handler := Recovery(http.Handler(mux))
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
