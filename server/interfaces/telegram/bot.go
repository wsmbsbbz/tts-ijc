package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

const (
	maxTelegramFileSize = 20 * 1024 * 1024 // 20 MB Telegram inbound limit
	longPollTimeout     = 30               // seconds for getUpdates long-polling
)

// BotConfig holds all dependencies needed by the BotServer.
type BotConfig struct {
	Token            string
	JobSvc           *application.JobService
	Storage          domain.FileStorage
	UserRepo         domain.UserRepository
	IDFunc           func() string
	AllowedProviders string // comma-separated list, e.g. "edge,gtts"
	UploadLimit      int64
}

// BotServer runs the Telegram bot update loop alongside the HTTP server.
type BotServer struct {
	api        *tgAPI
	cfg        BotConfig
	store      *stateStore
	notifier   *notifier
	httpClient *http.Client
}

// NewBotServer constructs a BotServer. The token is not validated here;
// the first call to getUpdates will fail if it is invalid.
func NewBotServer(cfg BotConfig) *BotServer {
	api := newTGAPI(cfg.Token)
	return &BotServer{
		api:   api,
		cfg:   cfg,
		store: newStateStore(),
		notifier: &notifier{
			api:     api,
			jobSvc:  cfg.JobSvc,
			storage: cfg.Storage,
		},
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Start runs the long-polling loop until ctx is cancelled.
func (b *BotServer) Start(ctx context.Context) {
	log.Println("tgbot: started (long-polling)")
	offset := 0
	for {
		select {
		case <-ctx.Done():
			log.Println("tgbot: stopped")
			return
		default:
		}

		updates, err := b.api.getUpdates(ctx, offset, longPollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("tgbot: get updates: %v", err)
			// Back off briefly to avoid hammering the API on repeated errors.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			b.dispatch(ctx, u)
		}
	}
}

// dispatch routes an incoming update to the appropriate handler.
func (b *BotServer) dispatch(ctx context.Context, u Update) {
	if u.CallbackQuery != nil {
		b.handleCallback(ctx, u.CallbackQuery)
		return
	}
	if u.Message == nil || u.Message.From == nil {
		return
	}
	msg := u.Message
	chatID := msg.Chat.ID

	user, err := findOrCreateUser(ctx, msg.From.ID, b.cfg.UserRepo, b.cfg.IDFunc)
	if err != nil {
		log.Printf("tgbot: find/create user %d: %v", msg.From.ID, err)
		b.api.sendMessage(ctx, chatID, "Internal error, please try again later.", nil) //nolint:errcheck
		return
	}

	sess := b.store.get(chatID)
	sess.userID = user.ID

	switch {
	case msg.Text != "" && strings.HasPrefix(msg.Text, "/"):
		b.handleCommand(ctx, chatID, sess, msg.Text)
	case msg.Document != nil || msg.Audio != nil || msg.Voice != nil || msg.Video != nil:
		b.handleFile(ctx, chatID, sess, msg)
	case msg.Text != "":
		b.handleText(ctx, chatID, sess, msg.Text)
	}
}

// --- Command handlers ---

const helpText = `<b>Translation Combinator Bot</b>

Convert VTT subtitles to speech and mix them into audio.

<b>Commands:</b>
/new — Start a new translation job
/status — Show your recent jobs
/cancel — Cancel the current operation
/help — Show this message

<b>How it works:</b>
1. /new → send your audio file (MP3/WAV/OGG/M4A, max 20 MB)
2. Send your WebVTT subtitle file (.vtt, max 20 MB)
3. Choose TTS provider and settings
4. Confirm — I'll notify you when done`

func (b *BotServer) handleCommand(ctx context.Context, chatID int64, sess *session, text string) {
	parts := strings.Fields(text)
	cmd := parts[0]
	// Strip @BotName suffix (group chats).
	if idx := strings.Index(cmd, "@"); idx != -1 {
		cmd = cmd[:idx]
	}
	var args string
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	switch cmd {
	case "/start", "/new":
		b.store.reset(chatID)
		fresh := b.store.get(chatID)
		fresh.userID = sess.userID
		fresh.state = stateWaitingAudio
		b.store.set(chatID, fresh)
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"👋 Welcome to <b>Translation Combinator Bot</b>!\n\n"+
				"Please send your audio file (MP3, WAV, OGG, M4A, FLAC…).\n"+
				"<i>Maximum file size: 20 MB</i>", nil)

	case "/cancel":
		b.store.reset(chatID)
		b.api.sendMessage(ctx, chatID, "✅ Cancelled. Use /new to start a new job.", nil) //nolint:errcheck

	case "/status":
		b.handleStatus(ctx, chatID, sess.userID, args)

	case "/help":
		b.api.sendMessage(ctx, chatID, helpText, nil) //nolint:errcheck

	default:
		b.api.sendMessage(ctx, chatID, "Unknown command. Use /help for available commands.", nil) //nolint:errcheck
	}
}

func (b *BotServer) handleStatus(ctx context.Context, chatID int64, userID, jobID string) {
	if userID == "" {
		b.api.sendMessage(ctx, chatID, "Use /new to start a job first.", nil) //nolint:errcheck
		return
	}

	if jobID != "" {
		job, err := b.cfg.JobSvc.GetJob(ctx, jobID)
		if err != nil || job.UserID != userID {
			b.api.sendMessage(ctx, chatID, "Job not found.", nil) //nolint:errcheck
			return
		}
		b.api.sendMessage(ctx, chatID, formatJob(job), nil) //nolint:errcheck
		return
	}

	jobs, err := b.cfg.JobSvc.ListJobs(ctx, userID, 5)
	if err != nil || len(jobs) == 0 {
		b.api.sendMessage(ctx, chatID, "No jobs found. Use /new to create one.", nil) //nolint:errcheck
		return
	}

	var sb strings.Builder
	sb.WriteString("<b>Your recent jobs:</b>\n\n")
	for _, j := range jobs {
		sb.WriteString(formatJob(j))
		sb.WriteString("\n\n")
	}
	b.api.sendMessage(ctx, chatID, sb.String(), nil) //nolint:errcheck
}

func formatJob(j domain.Job) string {
	status := string(j.Status)
	switch j.Status {
	case domain.StatusQueued:
		status = "⏳ queued"
	case domain.StatusProcessing:
		status = "⚙️ processing"
		if j.Progress != "" {
			status += " — " + j.Progress
		}
	case domain.StatusCompleted:
		status = "✅ completed"
	case domain.StatusFailed:
		status = "❌ failed"
		if j.Error != nil {
			status += ": " + *j.Error
		}
	}
	return fmt.Sprintf("<code>%s</code>\n%s | provider: %s\nStatus: %s",
		j.ID[:8], j.AudioName, j.Config.TTSProvider, status)
}

// --- File upload handlers ---

func (b *BotServer) handleFile(ctx context.Context, chatID int64, sess *session, msg *Message) {
	switch sess.state {
	case stateWaitingAudio:
		b.handleAudioUpload(ctx, chatID, sess, msg)
	case stateWaitingVTT:
		b.handleVTTUpload(ctx, chatID, sess, msg)
	default:
		b.api.sendMessage(ctx, chatID, "Unexpected file. Use /new to start a job or /cancel to reset.", nil) //nolint:errcheck
	}
}

func (b *BotServer) handleAudioUpload(ctx context.Context, chatID int64, sess *session, msg *Message) {
	var fileID, fileName, mimeType string
	var fileSize int64

	switch {
	case msg.Audio != nil:
		fileID = msg.Audio.FileID
		fileName = msg.Audio.FileName
		if fileName == "" {
			fileName = "audio.mp3"
		}
		mimeType = msg.Audio.MIMEType
		fileSize = msg.Audio.FileSize
	case msg.Voice != nil:
		fileID = msg.Voice.FileID
		fileName = "voice.ogg"
		mimeType = msg.Voice.MIMEType
		fileSize = msg.Voice.FileSize
	case msg.Video != nil:
		fileID = msg.Video.FileID
		fileName = msg.Video.FileName
		if fileName == "" {
			fileName = "video.mp4"
		}
		mimeType = msg.Video.MIMEType
		fileSize = msg.Video.FileSize
	case msg.Document != nil:
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
		mimeType = msg.Document.MIMEType
		fileSize = msg.Document.FileSize
	}

	if !isAudioFile(fileName, mimeType) {
		b.api.sendMessage(ctx, chatID, "Please send an audio file (MP3, WAV, OGG, M4A, FLAC, AAC, etc.).", nil) //nolint:errcheck
		return
	}
	if fileSize > maxTelegramFileSize {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			fmt.Sprintf("File too large (%.1f MB). Telegram allows up to 20 MB per file.",
				float64(fileSize)/(1024*1024)), nil)
		return
	}

	sess.audioFileID = fileID
	sess.audioName = fileName
	sess.audioSize = fileSize
	sess.state = stateWaitingVTT
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Audio received: <b>%s</b>\n\nNow send your WebVTT subtitle file (.vtt).", fileName), nil)
}

func (b *BotServer) handleVTTUpload(ctx context.Context, chatID int64, sess *session, msg *Message) {
	if msg.Document == nil {
		b.api.sendMessage(ctx, chatID, "Please send a WebVTT file (.vtt) as a document.", nil) //nolint:errcheck
		return
	}
	doc := msg.Document
	nameLower := strings.ToLower(doc.FileName)
	if !strings.HasSuffix(nameLower, ".vtt") && doc.MIMEType != "text/vtt" {
		b.api.sendMessage(ctx, chatID, "Please send a WebVTT (.vtt) subtitle file.", nil) //nolint:errcheck
		return
	}
	if doc.FileSize > maxTelegramFileSize {
		b.api.sendMessage(ctx, chatID, "Subtitle file too large (max 20 MB).", nil) //nolint:errcheck
		return
	}

	sess.vttFileID = doc.FileID
	sess.vttName = doc.FileName
	sess.vttSize = doc.FileSize
	sess.state = stateWaitingConfig
	sess.configStep = configStepProvider
	sess.cfg = domain.JobConfig{
		TTSVolume:   0.08,
		Concurrency: 3,
	}
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Subtitle received: <b>%s</b>\n\nSelect a TTS provider:", doc.FileName),
		b.providerKeyboard())
}

// --- Text input handler ---

func (b *BotServer) handleText(ctx context.Context, chatID int64, sess *session, text string) {
	if sess.state == stateWaitingConfig && sess.configStep == configStepVolume {
		vol, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || vol < 0 || vol > 1 {
			b.api.sendMessage(ctx, chatID, "Please enter a number between 0.0 and 1.0 (e.g. 0.08), or use a button.", nil) //nolint:errcheck
			return
		}
		sess.cfg.TTSVolume = vol
		sess.configStep = configStepSpeedup
		b.store.set(chatID, sess)
		b.api.sendMessage(ctx, chatID, "Enable speech acceleration?", b.speedupKeyboard()) //nolint:errcheck
		return
	}
	b.api.sendMessage(ctx, chatID, "Use /new to start a job or /help for help.", nil) //nolint:errcheck
}

// --- Callback query handler ---

func (b *BotServer) handleCallback(ctx context.Context, cq *CallbackQuery) {
	if cq.Message == nil || cq.From == nil {
		return
	}
	chatID := cq.Message.Chat.ID

	b.api.answerCallbackQuery(ctx, cq.ID, "") //nolint:errcheck

	user, err := findOrCreateUser(ctx, cq.From.ID, b.cfg.UserRepo, b.cfg.IDFunc)
	if err != nil {
		log.Printf("tgbot: find/create user in callback %d: %v", cq.From.ID, err)
		return
	}

	sess := b.store.get(chatID)
	sess.userID = user.ID

	data := cq.Data
	switch {
	case strings.HasPrefix(data, "provider:"):
		b.handleProviderCallback(ctx, chatID, sess, strings.TrimPrefix(data, "provider:"))
	case strings.HasPrefix(data, "volume:"):
		b.handleVolumeCallback(ctx, chatID, sess, strings.TrimPrefix(data, "volume:"))
	case strings.HasPrefix(data, "speedup:"):
		b.handleSpeedupCallback(ctx, chatID, sess, strings.TrimPrefix(data, "speedup:"))
	case data == "confirm":
		b.handleConfirm(ctx, chatID, sess)
	case data == "cancel_job":
		b.store.reset(chatID)
		b.api.sendMessage(ctx, chatID, "Cancelled. Use /new to start again.", nil) //nolint:errcheck
	}
}

func (b *BotServer) handleProviderCallback(ctx context.Context, chatID int64, sess *session, provider string) {
	if sess.state != stateWaitingConfig || sess.configStep != configStepProvider {
		return
	}
	allowed := b.allowedProviders()
	valid := false
	for _, p := range allowed {
		if p == provider {
			valid = true
			break
		}
	}
	if !valid {
		b.api.sendMessage(ctx, chatID, "Invalid provider. Please choose from the buttons.", nil) //nolint:errcheck
		return
	}

	sess.cfg.TTSProvider = provider
	sess.configStep = configStepVolume
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("Provider: <b>%s</b>\n\nChoose TTS volume (or type a number 0.0–1.0):", provider),
		b.volumeKeyboard())
}

func (b *BotServer) handleVolumeCallback(ctx context.Context, chatID int64, sess *session, val string) {
	if sess.state != stateWaitingConfig || sess.configStep != configStepVolume {
		return
	}
	vol, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return
	}
	sess.cfg.TTSVolume = vol
	sess.configStep = configStepSpeedup
	b.store.set(chatID, sess)
	b.api.sendMessage(ctx, chatID, "Enable speech acceleration?", b.speedupKeyboard()) //nolint:errcheck
}

func (b *BotServer) handleSpeedupCallback(ctx context.Context, chatID int64, sess *session, val string) {
	if sess.state != stateWaitingConfig || sess.configStep != configStepSpeedup {
		return
	}
	sess.cfg.NoSpeedup = (val == "off")
	sess.state = stateConfirming
	b.store.set(chatID, sess)

	speedupStr := "enabled"
	if sess.cfg.NoSpeedup {
		speedupStr = "disabled"
	}
	summary := fmt.Sprintf(
		"📋 <b>Job Summary</b>\n\n"+
			"Audio:        <code>%s</code>\n"+
			"Subtitle:     <code>%s</code>\n"+
			"TTS Provider: <b>%s</b>\n"+
			"Volume:       <b>%.2f</b>\n"+
			"Acceleration: <b>%s</b>\n\n"+
			"Start the job?",
		sess.audioName, sess.vttName,
		sess.cfg.TTSProvider, sess.cfg.TTSVolume, speedupStr,
	)
	b.api.sendMessage(ctx, chatID, summary, b.confirmKeyboard()) //nolint:errcheck
}

// handleConfirm downloads both files from Telegram, uploads them to R2,
// creates the job, and launches the progress notifier — all in a goroutine.
func (b *BotServer) handleConfirm(ctx context.Context, chatID int64, sess *session) {
	if sess.state != stateConfirming {
		return
	}

	// Snapshot session data before resetting.
	audioFileID := sess.audioFileID
	audioName := sess.audioName
	vttFileID := sess.vttFileID
	vttName := sess.vttName
	cfg := sess.cfg
	userID := sess.userID

	b.store.reset(chatID)
	b.api.sendMessage(ctx, chatID, "⏳ Uploading files…", nil) //nolint:errcheck

	go func() {
		bgCtx := context.Background()

		audioKey, err := b.uploadTelegramFile(bgCtx, audioFileID, userID, audioName)
		if err != nil {
			log.Printf("tgbot: upload audio for chat %d: %v", chatID, err)
			b.api.sendMessage(bgCtx, chatID, "❌ Failed to upload audio: "+err.Error(), nil) //nolint:errcheck
			return
		}

		vttKey, err := b.uploadTelegramFile(bgCtx, vttFileID, userID, vttName)
		if err != nil {
			log.Printf("tgbot: upload vtt for chat %d: %v", chatID, err)
			b.cfg.Storage.Delete(bgCtx, audioKey)                                              //nolint:errcheck
			b.api.sendMessage(bgCtx, chatID, "❌ Failed to upload subtitle: "+err.Error(), nil) //nolint:errcheck
			return
		}

		job, err := b.cfg.JobSvc.CreateJob(bgCtx, userID, audioKey, vttKey, audioName, vttName, cfg)
		if err != nil {
			log.Printf("tgbot: create job for chat %d: %v", chatID, err)
			b.cfg.Storage.Delete(bgCtx, audioKey)                                         //nolint:errcheck
			b.cfg.Storage.Delete(bgCtx, vttKey)                                           //nolint:errcheck
			b.api.sendMessage(bgCtx, chatID, "❌ Failed to create job: "+err.Error(), nil) //nolint:errcheck
			return
		}

		b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
			fmt.Sprintf("✅ Job queued! ID: <code>%s</code>\n\nI'll notify you when it's done.", job.ID), nil)

		b.notifier.watch(bgCtx, chatID, job.ID)
	}()
}

// uploadTelegramFile downloads fileID from Telegram and uploads it to R2.
// Returns the R2 object key.
func (b *BotServer) uploadTelegramFile(ctx context.Context, fileID, userID, fileName string) (string, error) {
	tgFile, err := b.api.getFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("get telegram file info: %w", err)
	}

	downloadURL := b.api.FileDownloadURL(tgFile.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download from telegram: %w", err)
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "tgbot-*-"+sanitizeFileName(fileName))
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

	key := path.Join("uploads", userID, b.cfg.IDFunc(), fileName)
	if err := b.cfg.Storage.Upload(ctx, tmpName, key); err != nil {
		return "", fmt.Errorf("upload to r2: %w", err)
	}

	// Track upload bytes for quota accounting.
	if tgFile.FileSize > 0 {
		b.cfg.UserRepo.IncrementUploadBytes(ctx, userID, tgFile.FileSize) //nolint:errcheck
	}

	return key, nil
}

// --- Inline keyboards ---

func (b *BotServer) allowedProviders() []string {
	if b.cfg.AllowedProviders == "" {
		return []string{"edge"}
	}
	parts := strings.Split(b.cfg.AllowedProviders, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (b *BotServer) providerKeyboard() *InlineKeyboardMarkup {
	providers := b.allowedProviders()
	var rows [][]InlineKeyboardButton
	for i := 0; i < len(providers); i += 2 {
		row := []InlineKeyboardButton{
			{Text: providers[i], CallbackData: "provider:" + providers[i]},
		}
		if i+1 < len(providers) {
			row = append(row, InlineKeyboardButton{
				Text: providers[i+1], CallbackData: "provider:" + providers[i+1],
			})
		}
		rows = append(rows, row)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *BotServer) volumeKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "Default (0.08)", CallbackData: "volume:0.08"}},
			{
				{Text: "Low (0.04)", CallbackData: "volume:0.04"},
				{Text: "Medium (0.12)", CallbackData: "volume:0.12"},
			},
			{
				{Text: "High (0.20)", CallbackData: "volume:0.20"},
				{Text: "Full (0.50)", CallbackData: "volume:0.50"},
			},
		},
	}
}

func (b *BotServer) speedupKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ Enable (recommended)", CallbackData: "speedup:on"},
				{Text: "Disable", CallbackData: "speedup:off"},
			},
		},
	}
}

func (b *BotServer) confirmKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🚀 Start Job", CallbackData: "confirm"},
				{Text: "❌ Cancel", CallbackData: "cancel_job"},
			},
		},
	}
}

// --- Helpers ---

// isAudioFile returns true if the file appears to be audio/video based on
// its MIME type or extension.
func isAudioFile(fileName, mimeType string) bool {
	if strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "video/") {
		return true
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(fileName), "."))
	switch ext {
	case "mp3", "m4a", "wav", "ogg", "flac", "aac", "opus", "wma",
		"mp4", "mkv", "webm", "mov", "avi":
		return true
	}
	return false
}

// sanitizeFileName replaces path separators so the name is safe to use in
// os.CreateTemp's pattern argument.
func sanitizeFileName(name string) string {
	return strings.NewReplacer("/", "_", "\\", "_").Replace(name)
}
