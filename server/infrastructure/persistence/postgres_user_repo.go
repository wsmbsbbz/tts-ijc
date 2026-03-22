package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// PostgresUserRepo implements domain.UserRepository using PostgreSQL.
type PostgresUserRepo struct {
	db *sql.DB
}

// PostgresSessionRepo implements domain.SessionRepository using PostgreSQL.
type PostgresSessionRepo struct {
	db *sql.DB
}

// NewPostgresUserRepos creates both user and session repos, sharing one *sql.DB.
// It also runs the required schema migrations.
func NewPostgresUserRepos(db *sql.DB) (*PostgresUserRepo, *PostgresSessionRepo, error) {
	if err := createPostgresUserTables(db); err != nil {
		return nil, nil, err
	}
	return &PostgresUserRepo{db: db}, &PostgresSessionRepo{db: db}, nil
}

func createPostgresUserTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL,
			expires_at    TIMESTAMPTZ NOT NULL,
			is_active     BOOLEAN NOT NULL DEFAULT TRUE
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create user/session tables: %w", err)
	}
	return nil
}

// --- UserRepository ---

func (r *PostgresUserRepo) Save(ctx context.Context, u domain.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, created_at, expires_at, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, u.ID, u.Username, u.PasswordHash, u.CreatedAt, u.ExpiresAt, u.IsActive)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PostgresUserRepo) FindByUsername(ctx context.Context, username string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, created_at, expires_at, is_active
		FROM users WHERE username = $1
	`, username)
	return scanPgUser(row)
}

func (r *PostgresUserRepo) FindByID(ctx context.Context, id string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, created_at, expires_at, is_active
		FROM users WHERE id = $1
	`, id)
	return scanPgUser(row)
}

func (r *PostgresUserRepo) CountActive(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_active = TRUE`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active users: %w", err)
	}
	return count, nil
}

func (r *PostgresUserRepo) DeactivateExpired(ctx context.Context) ([]string, error) {
	now := time.Now()
	rows, err := r.db.QueryContext(ctx, `
		UPDATE users SET is_active = FALSE
		WHERE is_active = TRUE AND expires_at < $1
		RETURNING id
	`, now)
	if err != nil {
		return nil, fmt.Errorf("deactivate expired users: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deactivated user id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanPgUser(s scanner) (domain.User, error) {
	var u domain.User
	err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.ExpiresAt, &u.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// --- SessionRepository ---

func (r *PostgresSessionRepo) Save(ctx context.Context, s domain.Session) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4)
	`, s.Token, s.UserID, s.CreatedAt, s.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepo) FindByToken(ctx context.Context, token string) (domain.Session, error) {
	var s domain.Session
	err := r.db.QueryRowContext(ctx, `
		SELECT token, user_id, created_at, expires_at FROM sessions WHERE token = $1
	`, token).Scan(&s.Token, &s.UserID, &s.CreatedAt, &s.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Session{}, domain.ErrSessionNotFound
		}
		return domain.Session{}, fmt.Errorf("find session: %w", err)
	}
	return s, nil
}

func (r *PostgresSessionRepo) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

func (r *PostgresSessionRepo) DeleteByUserIDs(ctx context.Context, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	// Build parameterized IN clause.
	args := make([]any, len(userIDs))
	placeholders := make([]byte, 0, len(userIDs)*5)
	for i, id := range userIDs {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = fmt.Appendf(placeholders, "$%d", i+1)
	}
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id IN ("+string(placeholders)+")",
		args...,
	)
	return err
}
