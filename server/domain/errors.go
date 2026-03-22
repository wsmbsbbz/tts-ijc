package domain

import "errors"

var (
	ErrJobNotFound = errors.New("job not found")
	ErrQueueFull   = errors.New("job queue is full")

	ErrUserNotFound         = errors.New("user not found")
	ErrUsernameExists       = errors.New("username already taken")
	ErrInvalidCredentials   = errors.New("invalid username or password")
	ErrSessionNotFound      = errors.New("session not found")
	ErrAccountExpired       = errors.New("account has expired")
	ErrRegisterLimitReached = errors.New("registration limit reached, please try again later")

	ErrUploadQuotaExceeded   = errors.New("upload quota exceeded")
	ErrDownloadQuotaExceeded = errors.New("download quota exceeded")
)
