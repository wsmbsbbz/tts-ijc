package domain

import "time"

// User is the domain entity representing a registered account.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	IsActive     bool
}

// Session represents an authenticated browser session.
type Session struct {
	Token     string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewUser creates an active User that expires after the given TTL.
func NewUser(id, username, passwordHash string, ttl time.Duration) User {
	now := time.Now()
	return User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
		IsActive:     true,
	}
}

// IsExpired reports whether the account's validity period has passed.
func (u User) IsExpired() bool {
	return time.Now().After(u.ExpiresAt)
}

// NewSession creates a Session that expires after the given TTL.
func NewSession(token, userID string, ttl time.Duration) Session {
	now := time.Now()
	return Session{
		Token:     token,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

// IsExpired reports whether the session has passed its expiry time.
func (s Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}
