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
}

// TelegramBindingRepository persists Telegram account bindings.
type TelegramBindingRepository interface {
	Save(ctx context.Context, b TelegramBinding) error
	FindByTelegramID(ctx context.Context, tgID int64) (TelegramBinding, error)
	DeleteByTelegramID(ctx context.Context, tgID int64) error
}
