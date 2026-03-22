package domain

import "time"

// Status represents the lifecycle state of a translation job.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// JobConfig holds TTS-related parameters for a translation job.
type JobConfig struct {
	TTSProvider string
	TTSVolume   float64
	NoSpeedup   bool
	Concurrency int
}

// Job is the core domain entity representing a translation task.
type Job struct {
	ID          string
	Status      Status
	Progress    string
	AudioKey    string
	VTTKey      string
	OutputKey   string
	AudioName   string
	VTTName     string
	Config      JobConfig
	CreatedAt   time.Time
	CompletedAt *time.Time
	Error       *string
}

// NewJob creates a Job in queued state with sensible defaults.
func NewJob(id, audioKey, vttKey, audioName, vttName string, cfg JobConfig) Job {
	if cfg.TTSProvider == "" {
		cfg.TTSProvider = "edge"
	}
	if cfg.TTSVolume <= 0 {
		cfg.TTSVolume = 0.08
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 5
	}

	return Job{
		ID:        id,
		Status:    StatusQueued,
		AudioKey:  audioKey,
		VTTKey:    vttKey,
		AudioName: audioName,
		VTTName:   vttName,
		Config:    cfg,
		CreatedAt: time.Now(),
	}
}

// MarkProcessing transitions the job to processing state.
func (j Job) MarkProcessing(progress string) Job {
	j.Status = StatusProcessing
	j.Progress = progress
	return j
}

// MarkCompleted transitions the job to completed state.
func (j Job) MarkCompleted(outputKey string) Job {
	now := time.Now()
	j.Status = StatusCompleted
	j.OutputKey = outputKey
	j.CompletedAt = &now
	return j
}

// MarkFailed transitions the job to failed state.
func (j Job) MarkFailed(errMsg string) Job {
	now := time.Now()
	j.Status = StatusFailed
	j.Error = &errMsg
	j.CompletedAt = &now
	return j
}

// UpdateProgress returns a copy with updated progress text.
func (j Job) UpdateProgress(progress string) Job {
	j.Progress = progress
	return j
}
