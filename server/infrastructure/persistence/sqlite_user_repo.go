package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// SQLiteUserRepo implements domain.UserRepository using SQLite.
type SQLiteUserRepo struct {
	db *sql.DB
}

// SQLiteSessionRepo implements domain.SessionRepository using SQLite.
type SQLiteSessionRepo struct {
	db *sql.DB
}

// NewSQLiteUserRepos creates both user and session repos, sharing one *sql.DB.
func NewSQLiteUserRepos(db *sql.DB) (*SQLiteUserRepo, *SQLiteSessionRepo, error) {
	if err := createSQLiteUserTables(db); err != nil {
		return nil, nil, err
	}
	return &SQLiteUserRepo{db: db}, &SQLiteSessionRepo{db: db}, nil
}

func createSQLiteUserTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at    TEXT NOT NULL,
			expires_at    TEXT NOT NULL,
			is_active     INTEGER NOT NULL DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create user/session tables: %w", err)
	}
	return nil
}

// --- UserRepository ---

func (r *SQLiteUserRepo) Save(ctx context.Context, u domain.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, created_at, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, u.PasswordHash,
		u.CreatedAt.Format(time.RFC3339),
		u.ExpiresAt.Format(time.RFC3339),
		boolToInt(u.IsActive),
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *SQLiteUserRepo) FindByUsername(ctx context.Context, username string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, created_at, expires_at, is_active
		FROM users WHERE username = ?
	`, username)
	return scanSQLiteUser(row)
}

func (r *SQLiteUserRepo) FindByID(ctx context.Context, id string) (domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, created_at, expires_at, is_active
		FROM users WHERE id = ?
	`, id)
	return scanSQLiteUser(row)
}

func (r *SQLiteUserRepo) CountActive(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_active = 1`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active users: %w", err)
	}
	return count, nil
}

func (r *SQLiteUserRepo) DeactivateExpired(ctx context.Context) ([]string, error) {
	now := time.Now().Format(time.RFC3339)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM users WHERE is_active = 1 AND expires_at < ?
	`, now)
	if err != nil {
		return nil, fmt.Errorf("query expired users: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired user id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(ids) > 0 {
		_, err = r.db.ExecContext(ctx, `
			UPDATE users SET is_active = 0 WHERE is_active = 1 AND expires_at < ?
		`, now)
		if err != nil {
			return nil, fmt.Errorf("deactivate expired users: %w", err)
		}
	}
	return ids, nil
}

func scanSQLiteUser(s scanner) (domain.User, error) {
	var (
		u         domain.User
		createdAt string
		expiresAt string
		isActive  int
	)
	err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt, &expiresAt, &isActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	u.IsActive = isActive != 0
	return u, nil
}

// --- SessionRepository ---

func (r *SQLiteSessionRepo) Save(ctx context.Context, s domain.Session) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, s.Token, s.UserID,
		s.CreatedAt.Format(time.RFC3339),
		s.ExpiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *SQLiteSessionRepo) FindByToken(ctx context.Context, token string) (domain.Session, error) {
	var (
		s         domain.Session
		createdAt string
		expiresAt string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT token, user_id, created_at, expires_at FROM sessions WHERE token = ?
	`, token).Scan(&s.Token, &s.UserID, &createdAt, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Session{}, domain.ErrSessionNotFound
		}
		return domain.Session{}, fmt.Errorf("find session: %w", err)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return s, nil
}

func (r *SQLiteSessionRepo) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (r *SQLiteSessionRepo) DeleteByUserIDs(ctx context.Context, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	// SQLite uses ? placeholders.
	args := make([]any, len(userIDs))
	placeholders := make([]byte, 0, len(userIDs)*2)
	for i, id := range userIDs {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id IN ("+string(placeholders)+")",
		args...,
	)
	return err
}
