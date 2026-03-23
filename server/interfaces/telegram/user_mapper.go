package telegram

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// botUserTTL gives Telegram-originated accounts a 10-year lifetime so they
// are never expired by the normal account-expiry loop.
const botUserTTL = 87600 * time.Hour

func tgUsername(tgUserID int64) string {
	return fmt.Sprintf("tg_%d", tgUserID)
}

// findOrCreateUser maps a Telegram user ID to a domain.User.
// If a binding exists the bound account is returned directly.
// Otherwise a dedicated tg_<id> account is auto-registered on first encounter.
func findOrCreateUser(
	ctx context.Context,
	tgUserID int64,
	userRepo domain.UserRepository,
	bindingRepo domain.TelegramBindingRepository,
	idFunc func() string,
) (domain.User, error) {
	// Check if this Telegram account is bound to an existing user.
	if binding, err := bindingRepo.FindByTelegramID(ctx, tgUserID); err == nil {
		user, err := userRepo.FindByID(ctx, binding.UserID)
		if err == nil {
			return user, nil
		}
		// Bound user was deleted; fall through to auto-register.
	} else if !errors.Is(err, domain.ErrTelegramBindingNotFound) {
		return domain.User{}, fmt.Errorf("find binding: %w", err)
	}

	// No binding — find or create a dedicated bot account.
	username := tgUsername(tgUserID)
	user, err := userRepo.FindByUsername(ctx, username)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, domain.ErrUserNotFound) {
		return domain.User{}, fmt.Errorf("find user: %w", err)
	}

	// Auto-register: generate a random password that is never exposed.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return domain.User{}, fmt.Errorf("generate password entropy: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	newUser := domain.NewUser(idFunc(), username, string(hash), botUserTTL)
	if err := userRepo.Save(ctx, newUser); err != nil {
		return domain.User{}, fmt.Errorf("save bot user: %w", err)
	}
	return newUser, nil
}
