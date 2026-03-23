package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// SQLiteTelegramBindingRepo implements domain.TelegramBindingRepository using SQLite.
type SQLiteTelegramBindingRepo struct {
	db *sql.DB
}

// NewSQLiteTelegramBindingRepo creates the repo and ensures the table exists.
// Pass the same *sql.DB used by SQLiteUserRepo to share the same file.
func NewSQLiteTelegramBindingRepo(db *sql.DB) (*SQLiteTelegramBindingRepo, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS telegram_bindings (
			telegram_id INTEGER PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			bound_at    TEXT NOT NULL
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("create telegram_bindings table: %w", err)
	}
	return &SQLiteTelegramBindingRepo{db: db}, nil
}

func (r *SQLiteTelegramBindingRepo) Save(ctx context.Context, b domain.TelegramBinding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO telegram_bindings (telegram_id, user_id, bound_at)
		VALUES (?, ?, ?)
		ON CONFLICT(telegram_id) DO UPDATE SET user_id = excluded.user_id, bound_at = excluded.bound_at
	`, b.TelegramID, b.UserID, b.BoundAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert telegram binding: %w", err)
	}
	return nil
}

func (r *SQLiteTelegramBindingRepo) FindByTelegramID(ctx context.Context, tgID int64) (domain.TelegramBinding, error) {
	var (
		b       domain.TelegramBinding
		boundAt string
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT telegram_id, user_id, bound_at FROM telegram_bindings WHERE telegram_id = ?
	`, tgID).Scan(&b.TelegramID, &b.UserID, &boundAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.TelegramBinding{}, domain.ErrTelegramBindingNotFound
		}
		return domain.TelegramBinding{}, fmt.Errorf("find telegram binding: %w", err)
	}
	b.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
	return b, nil
}

func (r *SQLiteTelegramBindingRepo) DeleteByTelegramID(ctx context.Context, tgID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM telegram_bindings WHERE telegram_id = ?`, tgID)
	return err
}
