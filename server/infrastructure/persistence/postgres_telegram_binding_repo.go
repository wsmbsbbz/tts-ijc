package persistence

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// PostgresTelegramBindingRepo implements domain.TelegramBindingRepository using PostgreSQL.
type PostgresTelegramBindingRepo struct {
	db *sql.DB
}

// NewPostgresTelegramBindingRepo creates the repo and ensures the table exists.
// Pass the same *sql.DB used by PostgresUserRepo to share the same connection pool.
func NewPostgresTelegramBindingRepo(db *sql.DB) (*PostgresTelegramBindingRepo, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS telegram_bindings (
			telegram_id BIGINT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			bound_at    TIMESTAMPTZ NOT NULL,
			asmr_token  TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("create telegram_bindings table: %w", err)
	}
	// Migrate existing tables that may not have the asmr_token column.
	db.Exec(`ALTER TABLE telegram_bindings ADD COLUMN IF NOT EXISTS asmr_token TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	return &PostgresTelegramBindingRepo{db: db}, nil
}

func (r *PostgresTelegramBindingRepo) Save(ctx context.Context, b domain.TelegramBinding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO telegram_bindings (telegram_id, user_id, bound_at, asmr_token)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE SET
			user_id    = excluded.user_id,
			bound_at   = excluded.bound_at,
			asmr_token = excluded.asmr_token
	`, b.TelegramID, b.UserID, b.BoundAt, b.AsmrToken)
	if err != nil {
		return fmt.Errorf("upsert telegram binding: %w", err)
	}
	return nil
}

func (r *PostgresTelegramBindingRepo) FindByTelegramID(ctx context.Context, tgID int64) (domain.TelegramBinding, error) {
	var b domain.TelegramBinding
	err := r.db.QueryRowContext(ctx, `
		SELECT telegram_id, user_id, bound_at, asmr_token FROM telegram_bindings WHERE telegram_id = $1
	`, tgID).Scan(&b.TelegramID, &b.UserID, &b.BoundAt, &b.AsmrToken)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.TelegramBinding{}, domain.ErrTelegramBindingNotFound
		}
		return domain.TelegramBinding{}, fmt.Errorf("find telegram binding: %w", err)
	}
	return b, nil
}

func (r *PostgresTelegramBindingRepo) DeleteByTelegramID(ctx context.Context, tgID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM telegram_bindings WHERE telegram_id = $1`, tgID)
	if err != nil {
		return fmt.Errorf("delete telegram binding: %w", err)
	}
	return nil
}

func (r *PostgresTelegramBindingRepo) ClearAsmrToken(ctx context.Context, tgID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE telegram_bindings SET asmr_token = '' WHERE telegram_id = $1`, tgID)
	if err != nil {
		return fmt.Errorf("clear asmr token: %w", err)
	}
	return nil
}

func (r *PostgresTelegramBindingRepo) SaveAsmrToken(ctx context.Context, tgID int64, userID, token string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO telegram_bindings (telegram_id, user_id, bound_at, asmr_token)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (telegram_id) DO UPDATE SET asmr_token = excluded.asmr_token
	`, tgID, userID, token)
	if err != nil {
		return fmt.Errorf("save asmr token: %w", err)
	}
	return nil
}
