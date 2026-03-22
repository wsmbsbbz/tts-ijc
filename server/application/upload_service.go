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
	storage      domain.FileStorage
	userRepo     domain.UserRepository
	idFunc       func() string
	uploadLimit  int64
}

// NewUploadService creates an UploadService.
func NewUploadService(storage domain.FileStorage, userRepo domain.UserRepository, idFunc func() string, uploadLimit int64) *UploadService {
	return &UploadService{
		storage:     storage,
		userRepo:    userRepo,
		idFunc:      idFunc,
		uploadLimit: uploadLimit,
	}
}

// UploadURLResult contains the presigned URL and the object key for the uploaded file.
type UploadURLResult struct {
	UploadURL string
	ObjectKey string
}

// RequestUploadURL generates a presigned PUT URL for direct browser-to-R2 upload.
// Files are stored under uploads/{userID}/{uuid}/{filename} so they can be
// batch-deleted by user prefix when an account is cleaned up.
// fileSize is pre-deducted from the user's upload quota before the URL is issued.
func (s *UploadService) RequestUploadURL(ctx context.Context, userID, filename, contentType string, fileSize int64) (UploadURLResult, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return UploadURLResult{}, fmt.Errorf("find user: %w", err)
	}
	if user.TotalBytesUploaded+fileSize > s.uploadLimit {
		return UploadURLResult{}, domain.ErrUploadQuotaExceeded
	}

	id := s.idFunc()
	key := path.Join("uploads", userID, id, filename)

	url, err := s.storage.GenerateUploadURL(ctx, key, contentType, uploadURLExpiry)
	if err != nil {
		return UploadURLResult{}, fmt.Errorf("generate upload url: %w", err)
	}

	if err := s.userRepo.IncrementUploadBytes(ctx, userID, fileSize); err != nil {
		return UploadURLResult{}, fmt.Errorf("increment upload bytes: %w", err)
	}

	return UploadURLResult{
		UploadURL: url,
		ObjectKey: key,
	}, nil
}
