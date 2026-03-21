package application

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const (
	jobTimeout       = 30 * time.Minute
	downloadURLExpiry = 1 * time.Hour
)

// WorkerService consumes job IDs from the queue and processes them.
type WorkerService struct {
	repo       domain.JobRepository
	storage    domain.FileStorage
	translator domain.Translator
	queue      <-chan string
}

// NewWorkerService creates a WorkerService.
func NewWorkerService(
	repo domain.JobRepository,
	storage domain.FileStorage,
	translator domain.Translator,
	queue <-chan string,
) *WorkerService {
	return &WorkerService{
		repo:       repo,
		storage:    storage,
		translator: translator,
		queue:      queue,
	}
}

// Start launches n worker goroutines. Blocks until ctx is cancelled.
func (w *WorkerService) Start(ctx context.Context, n int) {
	for i := range n {
		go w.runWorker(ctx, i)
	}
}

func (w *WorkerService) runWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case jobID, ok := <-w.queue:
			if !ok {
				return
			}
			w.processJob(ctx, workerID, jobID)
		}
	}
}

func (w *WorkerService) processJob(ctx context.Context, workerID int, jobID string) {
	log.Printf("[worker %d] processing job %s", workerID, jobID)

	job, err := w.repo.FindByID(ctx, jobID)
	if err != nil {
		log.Printf("[worker %d] job %s not found: %v", workerID, jobID, err)
		return
	}

	if err := w.repo.UpdateStatus(ctx, jobID, domain.StatusProcessing, "downloading files"); err != nil {
		log.Printf("[worker %d] job %s: failed to update status: %v", workerID, jobID, err)
		return
	}

	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("tc_job_%s_", jobID))
	if err != nil {
		w.failJob(ctx, jobID, fmt.Sprintf("create temp dir: %v", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	audioPath := filepath.Join(tmpDir, "input_audio")
	vttPath := filepath.Join(tmpDir, "input.vtt")
	outputPath := filepath.Join(tmpDir, "output.mp3")

	// Download input files from R2
	if err := w.storage.Download(ctx, job.AudioKey, audioPath); err != nil {
		w.failJob(ctx, jobID, fmt.Sprintf("download audio: %v", err))
		return
	}
	if err := w.storage.Download(ctx, job.VTTKey, vttPath); err != nil {
		w.failJob(ctx, jobID, fmt.Sprintf("download vtt: %v", err))
		return
	}

	// Run translation with timeout
	jobCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	input := domain.TranslateInput{
		AudioPath:  audioPath,
		VTTPath:    vttPath,
		OutputPath: outputPath,
		Config:     job.Config,
	}

	onProgress := func(p domain.TranslateProgress) {
		msg := fmt.Sprintf("[%d/%d] %s", p.Current, p.Total, p.Message)
		_ = w.repo.UpdateStatus(ctx, jobID, domain.StatusProcessing, msg)
	}

	if err := w.translator.Execute(jobCtx, input, onProgress); err != nil {
		w.failJob(ctx, jobID, fmt.Sprintf("translation failed: %v", err))
		return
	}

	// Upload result to R2
	_ = w.repo.UpdateStatus(ctx, jobID, domain.StatusProcessing, "uploading result")
	outputKey := fmt.Sprintf("outputs/%s/output.mp3", jobID)

	if err := w.storage.Upload(ctx, outputPath, outputKey); err != nil {
		w.failJob(ctx, jobID, fmt.Sprintf("upload result: %v", err))
		return
	}

	if err := w.repo.SetCompleted(ctx, jobID, outputKey); err != nil {
		log.Printf("[worker %d] job %s: failed to mark completed: %v", workerID, jobID, err)
		return
	}

	log.Printf("[worker %d] job %s completed", workerID, jobID)
}

func (w *WorkerService) failJob(ctx context.Context, jobID, errMsg string) {
	log.Printf("job %s failed: %s", jobID, errMsg)
	_ = w.repo.SetFailed(ctx, jobID, errMsg)
}

// CleanupExpired deletes expired jobs and their R2 objects.
func (w *WorkerService) CleanupExpired(ctx context.Context, ttl time.Duration) {
	expired, err := w.repo.DeleteExpired(ctx, ttl)
	if err != nil {
		log.Printf("cleanup: failed to delete expired jobs: %v", err)
		return
	}

	for _, job := range expired {
		_ = w.storage.Delete(ctx, job.AudioKey)
		_ = w.storage.Delete(ctx, job.VTTKey)
		if job.OutputKey != "" {
			_ = w.storage.Delete(ctx, job.OutputKey)
		}
	}

	if len(expired) > 0 {
		log.Printf("cleanup: removed %d expired jobs", len(expired))
	}
}

// StartCleanupLoop runs periodic cleanup in a goroutine.
func (w *WorkerService) StartCleanupLoop(ctx context.Context, interval, ttl time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.CleanupExpired(ctx, ttl)
			}
		}
	}()
}
