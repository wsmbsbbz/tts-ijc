package application

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const uploadURLExpiry = 15 * time.Minute

// UploadService handles presigned upload URL generation.
type UploadService struct {
	storage domain.FileStorage
	idFunc  func() string
}

// NewUploadService creates an UploadService.
func NewUploadService(storage domain.FileStorage, idFunc func() string) *UploadService {
	return &UploadService{storage: storage, idFunc: idFunc}
}

// UploadURLResult contains the presigned URL and the object key for the uploaded file.
type UploadURLResult struct {
	UploadURL string
	ObjectKey string
}

// RequestUploadURL generates a presigned PUT URL for direct browser-to-R2 upload.
// Files are stored under uploads/{userID}/{uuid}/{filename} so they can be
// batch-deleted by user prefix when an account is cleaned up.
func (s *UploadService) RequestUploadURL(ctx context.Context, userID, filename, contentType string) (UploadURLResult, error) {
	id := s.idFunc()
	key := path.Join("uploads", userID, id, filename)

	url, err := s.storage.GenerateUploadURL(ctx, key, contentType, uploadURLExpiry)
	if err != nil {
		return UploadURLResult{}, fmt.Errorf("generate upload url: %w", err)
	}

	return UploadURLResult{
		UploadURL: url,
		ObjectKey: key,
	}, nil
}
