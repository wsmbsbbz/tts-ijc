package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// AuthService handles user registration, login, logout, and session validation.
type AuthService struct {
	userRepo     domain.UserRepository
	sessionRepo  domain.SessionRepository
	idFunc       func() string
	accountTTL   time.Duration
	sessionTTL   time.Duration
	maxActive    int
}

// NewAuthService creates an AuthService.
// accountTTL controls how long a new account stays active.
// sessionTTL controls how long a session token is valid (should be <= accountTTL).
// maxActive is the maximum number of simultaneously active accounts.
func NewAuthService(
	userRepo domain.UserRepository,
	sessionRepo domain.SessionRepository,
	idFunc func() string,
	accountTTL, sessionTTL time.Duration,
	maxActive int,
) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		idFunc:      idFunc,
		accountTTL:  accountTTL,
		sessionTTL:  sessionTTL,
		maxActive:   maxActive,
	}
}

// Register creates a new user account and returns the initial session token.
// Returns ErrUsernameExists if the username is taken.
// Returns ErrRegisterLimitReached if the active account cap is hit.
func (s *AuthService) Register(ctx context.Context, username, password string) (domain.Session, error) {
	// Check the active account cap first (cheap).
	count, err := s.userRepo.CountActive(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("count active users: %w", err)
	}
	if count >= s.maxActive {
		return domain.Session{}, domain.ErrRegisterLimitReached
	}

	// Verify username uniqueness.
	if _, err := s.userRepo.FindByUsername(ctx, username); err == nil {
		return domain.Session{}, domain.ErrUsernameExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.Session{}, fmt.Errorf("hash password: %w", err)
	}

	user := domain.NewUser(s.idFunc(), username, string(hash), s.accountTTL)
	if err := s.userRepo.Save(ctx, user); err != nil {
		return domain.Session{}, fmt.Errorf("save user: %w", err)
	}

	return s.createSession(ctx, user.ID)
}

// Login verifies credentials and returns a new session token.
// Returns ErrInvalidCredentials for any auth failure (username not found, wrong password).
// Returns ErrAccountExpired if the account exists but has expired.
func (s *AuthService) Login(ctx context.Context, username, password string) (domain.Session, error) {
	user, err := s.userRepo.FindByUsername(ctx, username)
	if err != nil {
		// Return generic error so callers cannot enumerate usernames.
		return domain.Session{}, domain.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return domain.Session{}, domain.ErrInvalidCredentials
	}

	if !user.IsActive || user.IsExpired() {
		return domain.Session{}, domain.ErrAccountExpired
	}

	return s.createSession(ctx, user.ID)
}

// Logout deletes the session associated with token.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.sessionRepo.DeleteByToken(ctx, token)
}

// ValidateSession looks up the session by token and returns the owning User.
// Returns ErrSessionNotFound if the token is unknown or expired.
func (s *AuthService) ValidateSession(ctx context.Context, token string) (domain.User, error) {
	session, err := s.sessionRepo.FindByToken(ctx, token)
	if err != nil {
		return domain.User{}, domain.ErrSessionNotFound
	}
	if session.IsExpired() {
		_ = s.sessionRepo.DeleteByToken(ctx, token)
		return domain.User{}, domain.ErrSessionNotFound
	}

	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil {
		return domain.User{}, domain.ErrSessionNotFound
	}
	if !user.IsActive || user.IsExpired() {
		_ = s.sessionRepo.DeleteByToken(ctx, token)
		return domain.User{}, domain.ErrAccountExpired
	}

	return user, nil
}

// ExpireAccounts deactivates expired accounts and deletes their sessions.
// Should be called periodically (e.g., from the cleanup loop).
func (s *AuthService) ExpireAccounts(ctx context.Context) error {
	deactivated, err := s.userRepo.DeactivateExpired(ctx)
	if err != nil {
		return fmt.Errorf("deactivate expired users: %w", err)
	}
	if len(deactivated) == 0 {
		return nil
	}
	if err := s.sessionRepo.DeleteByUserIDs(ctx, deactivated); err != nil {
		return fmt.Errorf("delete sessions for expired users: %w", err)
	}
	return nil
}

// createSession generates a cryptographically random token and persists it.
func (s *AuthService) createSession(ctx context.Context, userID string) (domain.Session, error) {
	token, err := generateToken()
	if err != nil {
		return domain.Session{}, fmt.Errorf("generate token: %w", err)
	}

	session := domain.NewSession(token, userID, s.sessionTTL)
	if err := s.sessionRepo.Save(ctx, session); err != nil {
		return domain.Session{}, fmt.Errorf("save session: %w", err)
	}
	return session, nil
}

// generateToken returns a 32-byte cryptographically random hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
