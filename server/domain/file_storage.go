package domain

import (
	"context"
	"time"
)

// FileStorage abstracts object storage operations (e.g. Cloudflare R2).
type FileStorage interface {
	GenerateUploadURL(ctx context.Context, key, contentType string, expiry time.Duration) (string, error)
	// GenerateDownloadURL returns a presigned GET URL. When filename is non-empty,
	// the response includes Content-Disposition: attachment; filename="<filename>".
	GenerateDownloadURL(ctx context.Context, key string, expiry time.Duration, filename string) (string, error)
	Download(ctx context.Context, key, localPath string) error
	Upload(ctx context.Context, localPath, key string) error
	Delete(ctx context.Context, key string) error
}
