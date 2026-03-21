package application

import (
	"context"
	"fmt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// JobService handles job creation, retrieval, and listing.
type JobService struct {
	repo    domain.JobRepository
	queue   chan<- string
	idFunc  func() string
	queueCap int
}

// NewJobService creates a JobService.
// queue is a send-only channel for dispatching job IDs to workers.
// idFunc generates unique job IDs.
func NewJobService(repo domain.JobRepository, queue chan<- string, idFunc func() string) *JobService {
	return &JobService{
		repo:   repo,
		queue:  queue,
		idFunc: idFunc,
	}
}

// CreateJob validates config, persists a new job, and enqueues it for processing.
func (s *JobService) CreateJob(ctx context.Context, audioKey, vttKey string, cfg domain.JobConfig) (domain.Job, error) {
	id := s.idFunc()
	job := domain.NewJob(id, audioKey, vttKey, cfg)

	if err := s.repo.Save(ctx, job); err != nil {
		return domain.Job{}, fmt.Errorf("save job: %w", err)
	}

	select {
	case s.queue <- job.ID:
		return job, nil
	default:
		// Queue is full — mark the job as failed and return error.
		_ = s.repo.SetFailed(ctx, job.ID, "server is busy, please try again later")
		return domain.Job{}, domain.ErrQueueFull
	}
}

// GetJob retrieves a single job by ID.
func (s *JobService) GetJob(ctx context.Context, id string) (domain.Job, error) {
	return s.repo.FindByID(ctx, id)
}

// ListJobs returns the most recent jobs.
func (s *JobService) ListJobs(ctx context.Context, limit int) ([]domain.Job, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.repo.ListRecent(ctx, limit)
}
