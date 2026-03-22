package domain

import "context"

// UserRepository defines persistence operations for User entities.
type UserRepository interface {
	Save(ctx context.Context, user User) error
	FindByUsername(ctx context.Context, username string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
	CountActive(ctx context.Context) (int, error)
	// DeactivateExpired marks expired accounts as inactive and returns their IDs
	// so the caller can cascade-delete their sessions.
	DeactivateExpired(ctx context.Context) ([]string, error)
}

// SessionRepository defines persistence operations for Session entities.
type SessionRepository interface {
	Save(ctx context.Context, session Session) error
	FindByToken(ctx context.Context, token string) (Session, error)
	DeleteByToken(ctx context.Context, token string) error
	DeleteByUserIDs(ctx context.Context, userIDs []string) error
}
