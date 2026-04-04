package telegram

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const (
	pollInterval      = 5 * time.Second
	downloadURLExpiry = 24 * time.Hour
	editDebounce      = 3 * time.Second // minimum interval between message edits
)

// WorkMeta holds work metadata passed from the RJ handler to the notifier.
type WorkMeta struct {
	Workno   string
	Title    string
	Circle   string
	VAs      []string
	Tags     []string
	CoverURL string
}

type notifier struct {
	api         *tgAPI
	jobSvc      *application.JobService
	storage     domain.FileStorage
	maxSendSize int64
}

// watch polls jobID every 5 seconds until it reaches a terminal state.
// Progress updates edit a single message in-place instead of sending new messages.
func (n *notifier) watch(ctx context.Context, chatID int64, jobID string) {
	msgID, err := n.api.sendMessageGetID(ctx, chatID, "⏳ Queued…", nil)
	if err != nil {
		log.Printf("tgbot: send initial progress msg: %v", err)
		return
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastText string
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

			switch job.Status {
			case domain.StatusCompleted:
				n.api.editMessageText(ctx, chatID, msgID, "✅ Completed!", nil) //nolint:errcheck
				n.deliver(ctx, chatID, job)
				return
			case domain.StatusFailed:
				msg := "unknown error"
				if job.Error != nil {
					msg = *job.Error
				}
				n.api.editMessageText(ctx, chatID, msgID, "❌ Job failed: "+msg, nil) //nolint:errcheck
				return
			default:
				if job.Progress != "" && job.Progress != lastText {
					lastText = job.Progress
					text := "⏳ " + job.Progress
					n.api.editMessageText(ctx, chatID, msgID, text, nil) //nolint:errcheck
				}
			}
		}
	}
}

// progressUpdate carries a status change from a watcher goroutine to the
// consolidated progress renderer.
type progressUpdate struct {
	index    int
	progress string // latest progress text (empty if terminal)
	done     bool
	failed   bool
	errMsg   string
	job      domain.Job
}

// watchOrdered watches multiple jobs in parallel, consolidating all progress
// into a single editable message. After all jobs finish, delivers all
// successful results in original order.
func (n *notifier) watchOrdered(ctx context.Context, chatID int64, jobIDs []string, audioNames []string, meta *WorkMeta) {
	count := len(jobIDs)

	// Send initial consolidated progress message.
	initText := n.buildProgressText(audioNames, make([]string, count), make([]bool, count), make([]bool, count), make([]string, count), meta)
	progressMsgID, err := n.api.sendMessageGetID(ctx, chatID, initText, nil)
	if err != nil {
		log.Printf("tgbot: send initial progress: %v", err)
		return
	}

	// Channel for progress updates from all watchers.
	updates := make(chan progressUpdate, count*2)

	// Launch watchers.
	for i, jobID := range jobIDs {
		go n.watchAndSignal(ctx, jobID, i, updates)
	}

	// Track per-job state.
	progresses := make([]string, count) // latest progress text per job
	completed := make([]bool, count)
	failed := make([]bool, count)
	errMsgs := make([]string, count)
	jobs := make([]domain.Job, count) // terminal jobs (for delivery)
	doneCount := 0

	// Debounce timer for edits.
	var editTimer *time.Timer
	var editPending bool
	var mu sync.Mutex

	flushEdit := func() {
		mu.Lock()
		editPending = false
		mu.Unlock()
		text := n.buildProgressText(audioNames, progresses, completed, failed, errMsgs, meta)
		n.api.editMessageText(ctx, chatID, progressMsgID, text, nil) //nolint:errcheck
	}

	scheduleEdit := func() {
		mu.Lock()
		defer mu.Unlock()
		if editPending {
			return
		}
		editPending = true
		if editTimer == nil {
			editTimer = time.AfterFunc(editDebounce, flushEdit)
		} else {
			editTimer.Reset(editDebounce)
		}
	}

	for doneCount < count {
		select {
		case <-ctx.Done():
			return
		case u := <-updates:
			if u.done || u.failed {
				completed[u.index] = u.done
				failed[u.index] = u.failed
				errMsgs[u.index] = u.errMsg
				if u.done {
					jobs[u.index] = u.job
				}
				doneCount++
				// Immediately flush on terminal events.
				if editTimer != nil {
					editTimer.Stop()
				}
				flushEdit()
			} else {
				progresses[u.index] = u.progress
				scheduleEdit()
			}
		}
	}

	if editTimer != nil {
		editTimer.Stop()
	}

	// Build final summary.
	successCount := 0
	for _, c := range completed {
		if c {
			successCount++
		}
	}
	finalText := n.buildFinalText(audioNames, completed, failed, errMsgs, meta, successCount, count)
	n.api.editMessageText(ctx, chatID, progressMsgID, finalText, nil) //nolint:errcheck

	// Deliver all successful results in order.
	// Collect completed jobs in order for batch delivery.
	var completedJobs []domain.Job
	for i := 0; i < count; i++ {
		if completed[i] {
			completedJobs = append(completedJobs, jobs[i])
		}
	}
	n.deliverBatch(ctx, chatID, completedJobs, meta)
}

// watchAndSignal polls a single job and sends updates to the shared channel.
func (n *notifier) watchAndSignal(ctx context.Context, jobID string, index int, updates chan<- progressUpdate) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastProgress string
	for {
		select {
		case <-ctx.Done():
			updates <- progressUpdate{index: index, failed: true, errMsg: "context cancelled"}
			return
		case <-ticker.C:
			job, err := n.jobSvc.GetJob(ctx, jobID)
			if err != nil {
				log.Printf("tgbot: poll job %s: %v", jobID, err)
				continue
			}

			if job.Progress != lastProgress && job.Progress != "" {
				lastProgress = job.Progress
				updates <- progressUpdate{index: index, progress: job.Progress}
			}

			switch job.Status {
			case domain.StatusCompleted:
				updates <- progressUpdate{index: index, done: true, job: job}
				return
			case domain.StatusFailed:
				msg := "unknown error"
				if job.Error != nil {
					msg = *job.Error
				}
				updates <- progressUpdate{index: index, failed: true, errMsg: msg}
				return
			}
		}
	}
}

// buildProgressText renders the consolidated progress message.
func (n *notifier) buildProgressText(audioNames, progresses []string, completed, failed []bool, errMsgs []string, meta *WorkMeta) string {
	var sb strings.Builder
	label := ""
	if meta != nil {
		label = " from " + meta.Workno
	}
	sb.WriteString(fmt.Sprintf("⚙️ Processing %d job(s)%s\n", len(audioNames), label))

	for i, name := range audioNames {
		sb.WriteByte('\n')
		if completed[i] {
			sb.WriteString(fmt.Sprintf("%d. ✅ %s", i+1, name))
		} else if failed[i] {
			sb.WriteString(fmt.Sprintf("%d. ❌ %s — %s", i+1, name, errMsgs[i]))
		} else if progresses[i] != "" {
			sb.WriteString(fmt.Sprintf("%d. ⏳ %s — %s", i+1, name, progresses[i]))
		} else {
			sb.WriteString(fmt.Sprintf("%d. 🕐 %s — queued", i+1, name))
		}
	}
	return sb.String()
}

// buildFinalText renders the final summary after all jobs complete.
func (n *notifier) buildFinalText(audioNames []string, completed, failed []bool, errMsgs []string, meta *WorkMeta, successCount, total int) string {
	var sb strings.Builder

	label := ""
	if meta != nil {
		label = meta.Workno + " — "
	}
	sb.WriteString(fmt.Sprintf("✅ %s%d/%d completed\n", label, successCount, total))

	for i, name := range audioNames {
		sb.WriteByte('\n')
		ext := path.Ext(name)
		base := name[:len(name)-len(ext)]
		outputName := base + "_translated.mp3"
		if completed[i] {
			sb.WriteString(fmt.Sprintf("%d. ✅ %s", i+1, outputName))
		} else if failed[i] {
			sb.WriteString(fmt.Sprintf("%d. ❌ %s — %s", i+1, name, errMsgs[i]))
		}
	}
	return sb.String()
}

// deliverBatch sends all completed jobs as a single Telegram audio media group
// when there are 2 or more files. For a single file it falls back to deliver().
// If the batch send fails it falls back to individual deliver() calls.
func (n *notifier) deliverBatch(ctx context.Context, chatID int64, jobs []domain.Job, meta *WorkMeta) {
	if len(jobs) == 0 {
		return
	}
	if len(jobs) == 1 {
		n.deliver(ctx, chatID, jobs[0])
		return
	}

	// Batch into groups of 10 (Telegram's sendMediaGroup limit).
	for start := 0; start < len(jobs); start += 10 {
		end := start + 10
		if end > len(jobs) {
			end = len(jobs)
		}
		batch := jobs[start:end]
		if err := n.sendBatch(ctx, chatID, batch, meta, start == 0); err != nil {
			log.Printf("tgbot: send batch (start=%d): %v, falling back to individual sends", start, err)
			for _, job := range batch {
				n.deliver(ctx, chatID, job)
			}
		}
	}
}

// sendBatch downloads a slice of jobs to temp files and sends them as one media group.
// firstBatch indicates whether this is the first batch (caption is added to first item).
func (n *notifier) sendBatch(ctx context.Context, chatID int64, jobs []domain.Job, meta *WorkMeta, firstBatch bool) error {
	// Download all outputs to temp files.
	type tempEntry struct {
		path     string
		filename string
	}
	entries := make([]tempEntry, 0, len(jobs))
	cleanup := func() {
		for _, e := range entries {
			os.Remove(e.path)
		}
	}

	for _, job := range jobs {
		ext := path.Ext(job.AudioName)
		base := job.AudioName[:len(job.AudioName)-len(ext)]
		outputName := base + "_translated.mp3"

		tmp, err := os.CreateTemp("", "tg_batch_*.mp3")
		if err != nil {
			cleanup()
			return fmt.Errorf("create temp: %w", err)
		}
		tmpPath := tmp.Name()
		tmp.Close()

		if err := n.storage.Download(ctx, job.OutputKey, tmpPath); err != nil {
			os.Remove(tmpPath)
			cleanup()
			return fmt.Errorf("download %s: %w", job.ID, err)
		}
		entries = append(entries, tempEntry{path: tmpPath, filename: outputName})
	}
	defer cleanup()

	// Build localAudio slice.
	audios := make([]localAudio, len(entries))
	for i, e := range entries {
		audios[i] = localAudio{path: e.path, filename: e.filename}
	}
	// Set caption on the very first item of the first batch.
	if firstBatch && meta != nil && meta.Workno != "" {
		audios[0].caption = fmt.Sprintf("✅ <b>%s</b> — %d file(s)", meta.Workno, len(jobs))
	}

	return n.api.sendAudioGroupMultipart(ctx, chatID, audios)
}

func (n *notifier) deliver(ctx context.Context, chatID int64, job domain.Job) {
	ext := path.Ext(job.AudioName)
	base := job.AudioName[:len(job.AudioName)-len(ext)]
	outputName := base + "_translated.mp3"

	caption := fmt.Sprintf("✅ <b>%s</b>", outputName)
	if err := n.sendDirect(ctx, chatID, job, outputName, caption); err != nil {
		log.Printf("tgbot: direct send for job %s: %v, falling back to link", job.ID, err)
	} else {
		return
	}

	// Fallback: R2 presigned download link.
	url, err := n.storage.GenerateDownloadURL(ctx, job.OutputKey, downloadURLExpiry, outputName)
	if err != nil {
		log.Printf("tgbot: generate download url for job %s: %v", job.ID, err)
		n.api.sendMessage(ctx, chatID, "✅ Job complete, but failed to generate download link.", nil) //nolint:errcheck
		return
	}
	n.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Done! Download (24 h):\n%s", url), nil)
}

// sendDirect downloads the output from R2 to a temp file and uploads it
// directly to Telegram via multipart. The temp file is removed afterwards.
func (n *notifier) sendDirect(ctx context.Context, chatID int64, job domain.Job, filename, caption string) error {
	tmp, err := os.CreateTemp("", "tg_deliver_*.mp3")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := n.storage.Download(ctx, job.OutputKey, tmpPath); err != nil {
		return fmt.Errorf("download from r2: %w", err)
	}
	return n.api.sendDocumentMultipart(ctx, chatID, tmpPath, filename, caption)
}
