package domain

import (
	"context"
	"time"
)

// JobRepository defines persistence operations for Job entities.
type JobRepository interface {
	Save(ctx context.Context, job Job) error
	FindByID(ctx context.Context, id string) (Job, error)
	ListRecent(ctx context.Context, userID string, limit int) ([]Job, error)
	UpdateStatus(ctx context.Context, id string, status Status, progress string) error
	SetCompleted(ctx context.Context, id string, outputKey string, outputSize int64) error
	SetFailed(ctx context.Context, id string, errMsg string) error
	DeleteExpired(ctx context.Context, ttl time.Duration) ([]Job, error)
}
