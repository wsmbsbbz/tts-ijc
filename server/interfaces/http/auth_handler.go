package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// AuthHandler handles registration, login, logout, and user profile.
type AuthHandler struct {
	authSvc              *application.AuthService
	uploadLimitBytes     int64
	downloadLimitBytes   int64
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(authSvc *application.AuthService, uploadLimit, downloadLimit int64) *AuthHandler {
	return &AuthHandler{
		authSvc:            authSvc,
		uploadLimitBytes:   uploadLimit,
		downloadLimitBytes: downloadLimit,
	}
}

// HandleMe handles GET /api/me.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _ := UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, MeResponse{
		Username:             user.Username,
		ExpiresAt:            user.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		TotalBytesUploaded:   user.TotalBytesUploaded,
		UploadLimitBytes:     h.uploadLimitBytes,
		TotalBytesDownloaded: user.TotalBytesDownloaded,
		DownloadLimitBytes:   h.downloadLimitBytes,
	})
}

// HandleRegister handles POST /api/auth/register.
func (h *AuthHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 32 {
		writeError(w, http.StatusBadRequest, "username must be 3–32 characters")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	session, err := h.authSvc.Register(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUsernameExists):
			writeError(w, http.StatusConflict, "username already taken")
		case errors.Is(err, domain.ErrRegisterLimitReached):
			writeError(w, http.StatusServiceUnavailable, "registration is temporarily unavailable, please try again later")
		default:
			writeError(w, http.StatusInternalServerError, "registration failed")
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{Token: session.Token, ExpiresAt: session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")})
}

// HandleLogin handles POST /api/auth/login.
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := h.authSvc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid username or password")
		case errors.Is(err, domain.ErrAccountExpired):
			writeError(w, http.StatusForbidden, "account has expired")
		default:
			writeError(w, http.StatusInternalServerError, "login failed")
		}
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{Token: session.Token, ExpiresAt: session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")})
}

// HandleLogout handles POST /api/auth/logout.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	token := extractBearerToken(r)
	if token != "" {
		_ = h.authSvc.Logout(r.Context(), token)
	}
	w.WriteHeader(http.StatusNoContent)
}
