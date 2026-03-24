package domain

import (
	"context"
	"errors"
	"time"
)

// ErrTelegramBindingNotFound is returned when no binding exists for the given Telegram ID.
var ErrTelegramBindingNotFound = errors.New("telegram binding not found")

// TelegramBinding links a Telegram user ID to an internal user account.
type TelegramBinding struct {
	TelegramID int64
	UserID     string
	BoundAt    time.Time
	AsmrToken  string // JWT token for asmr.one API (empty if not bound)
}

// TelegramBindingRepository persists Telegram account bindings.
type TelegramBindingRepository interface {
	Save(ctx context.Context, b TelegramBinding) error
	FindByTelegramID(ctx context.Context, tgID int64) (TelegramBinding, error)
	DeleteByTelegramID(ctx context.Context, tgID int64) error
	// SaveAsmrToken upserts the asmr.one JWT for the given Telegram user.
	// userID is required for the insert path when no binding row exists yet.
	SaveAsmrToken(ctx context.Context, tgID int64, userID, token string) error
	// ClearAsmrToken removes the stored asmr.one token for the given Telegram user.
	// It is a no-op if no binding exists.
	ClearAsmrToken(ctx context.Context, tgID int64) error
}
