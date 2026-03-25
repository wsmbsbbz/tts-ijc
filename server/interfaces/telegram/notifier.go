package telegram

import (
	"context"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const (
	pollInterval      = 5 * time.Second
	maxTGSendSize     = 50 * 1024 * 1024 // 50 MB Telegram outbound limit
	downloadURLExpiry = 24 * time.Hour
)

type notifier struct {
	api     *tgAPI
	jobSvc  *application.JobService
	storage domain.FileStorage
}

// watch polls jobID every 5 seconds until it reaches a terminal state,
// sending progress and result messages to chatID via Telegram.
func (n *notifier) watch(ctx context.Context, chatID int64, jobID string) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastProgress string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := n.jobSvc.GetJob(ctx, jobID)
			if err != nil {
				log.Printf("tgbot: poll job %s: %v", jobID, err)
				continue
			}

			if job.Progress != lastProgress && job.Progress != "" {
				lastProgress = job.Progress
				if err := n.api.sendMessage(ctx, chatID, "⏳ "+job.Progress, nil); err != nil {
					log.Printf("tgbot: send progress: %v", err)
				}
			}

			switch job.Status {
			case domain.StatusCompleted:
				n.deliver(ctx, chatID, job)
				return
			case domain.StatusFailed:
				msg := "unknown error"
				if job.Error != nil {
					msg = *job.Error
				}
				n.api.sendMessage(ctx, chatID, "❌ Job failed: "+msg, nil) //nolint:errcheck
				return
			}
		}
	}
}

// jobCompletion carries the terminal result of a single job for ordered delivery.
type jobCompletion struct {
	job    domain.Job
	failed bool
	errMsg string
}

// watchOrdered watches multiple jobs in parallel (sending progress as each
// advances) and delivers completion notifications in the original jobIDs order.
func (n *notifier) watchOrdered(ctx context.Context, chatID int64, jobIDs []string) {
	completions := make([]chan jobCompletion, len(jobIDs))
	for i := range completions {
		completions[i] = make(chan jobCompletion, 1)
	}
	for i, jobID := range jobIDs {
		go n.watchAndSignal(ctx, chatID, jobID, completions[i])
	}
	for _, ch := range completions {
		result := <-ch
		if result.failed {
			n.api.sendMessage(ctx, chatID, "❌ Job failed: "+result.errMsg, nil) //nolint:errcheck
		} else {
			n.deliver(ctx, chatID, result.job)
		}
	}
}

// watchAndSignal is like watch but sends the terminal result to done instead of
// delivering it directly, so the caller can sequence deliveries.
func (n *notifier) watchAndSignal(ctx context.Context, chatID int64, jobID string, done chan<- jobCompletion) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastProgress string
	for {
		select {
		case <-ctx.Done():
			done <- jobCompletion{failed: true, errMsg: "context cancelled"}
			return
		case <-ticker.C:
			job, err := n.jobSvc.GetJob(ctx, jobID)
			if err != nil {
				log.Printf("tgbot: poll job %s: %v", jobID, err)
				continue
			}

			if job.Progress != lastProgress && job.Progress != "" {
				lastProgress = job.Progress
				if err := n.api.sendMessage(ctx, chatID, "⏳ "+job.Progress, nil); err != nil {
					log.Printf("tgbot: send progress: %v", err)
				}
			}

			switch job.Status {
			case domain.StatusCompleted:
				done <- jobCompletion{job: job}
				return
			case domain.StatusFailed:
				msg := "unknown error"
				if job.Error != nil {
					msg = *job.Error
				}
				done <- jobCompletion{failed: true, errMsg: msg}
				return
			}
		}
	}
}

func (n *notifier) deliver(ctx context.Context, chatID int64, job domain.Job) {
	ext := path.Ext(job.AudioName)
	base := job.AudioName[:len(job.AudioName)-len(ext)]
	outputName := base + "_translated.mp3"

	url, err := n.storage.GenerateDownloadURL(ctx, job.OutputKey, downloadURLExpiry, outputName)
	if err != nil {
		log.Printf("tgbot: generate download url for job %s: %v", job.ID, err)
		n.api.sendMessage(ctx, chatID, "✅ Job complete, but failed to generate download link.", nil) //nolint:errcheck
		return
	}

	if job.OutputSize > 0 && job.OutputSize <= maxTGSendSize {
		caption := fmt.Sprintf("✅ Done! <b>%s</b>", outputName)
		if err := n.api.sendDocument(ctx, chatID, url, caption); err != nil {
			log.Printf("tgbot: send document: %v", err)
			// Fall back to a plain download link.
			n.api.sendMessage(ctx, chatID, fmt.Sprintf("✅ Done! Download (24 h):\n%s", url), nil) //nolint:errcheck
		}
		return
	}

	sizeMB := float64(job.OutputSize) / (1024 * 1024)
	n.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Done! File is %.1f MB (Telegram limit 50 MB).\nDownload link (24 h):\n%s", sizeMB, url), nil)
}
