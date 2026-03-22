package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2Storage implements domain.FileStorage using Cloudflare R2 (S3-compatible).
type R2Storage struct {
	client     *s3.Client
	presigner  *s3.PresignClient
	bucketName string
}

// NewR2Storage creates an R2Storage client.
func NewR2Storage(endpoint, accessKeyID, secretAccessKey, bucketName string) *R2Storage {
	cfg := aws.Config{
		Region: "auto",
		Credentials: credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			"",
		),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
	})

	return &R2Storage{
		client:     client,
		presigner:  s3.NewPresignClient(client),
		bucketName: bucketName,
	}
}

func (s *R2Storage) GenerateUploadURL(ctx context.Context, key, contentType string, expiry time.Duration) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:      &s.bucketName,
		Key:         &key,
		ContentType: &contentType,
	}

	resp, err := s.presigner.PresignPutObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return resp.URL, nil
}

func (s *R2Storage) GenerateDownloadURL(ctx context.Context, key string, expiry time.Duration, filename string) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	}
	if filename != "" {
		cd := `attachment; filename="` + filename + `"`
		input.ResponseContentDisposition = &cd
	}

	resp, err := s.presigner.PresignGetObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return resp.URL, nil
}

func (s *R2Storage) Download(ctx context.Context, key, localPath string) error {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (s *R2Storage) Upload(ctx context.Context, localPath, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

func (s *R2Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucketName,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}
