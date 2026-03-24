package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/asmrone"
)

var rjPattern = regexp.MustCompile(`(?i)^RJ\d+$`)

// handleAsmrBind saves the user's asmr.one JWT token.
func (b *BotServer) handleAsmrBind(ctx context.Context, chatID int64, tgUserID int64, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"Usage: <code>/asmr_bind &lt;your_jwt_token&gt;</code>\n\n"+
				"Paste your asmr.one JWT token to enable RJ-based jobs.\n"+
				"The token is valid for ~1 year.", nil)
		return
	}
	if err := b.cfg.BindingRepo.SaveAsmrToken(ctx, tgUserID, token); err != nil {
		log.Printf("tgbot: save asmr token for tg %d: %v", tgUserID, err)
		b.api.sendMessage(ctx, chatID, "❌ Failed to save token, please try again.", nil) //nolint:errcheck
		return
	}
	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		"✅ asmr.one token saved.\n\nUse /rj <code>RJxxxxxx</code> to start a job from asmr.one.", nil)
}

// handleAsmrUnbind removes the user's stored asmr.one JWT token.
func (b *BotServer) handleAsmrUnbind(ctx context.Context, chatID int64, tgUserID int64) {
	if err := b.cfg.BindingRepo.SaveAsmrToken(ctx, tgUserID, ""); err != nil {
		b.api.sendMessage(ctx, chatID, "❌ Failed to remove token, please try again.", nil) //nolint:errcheck
		return
	}
	b.api.sendMessage(ctx, chatID, "✅ asmr.one token removed.", nil) //nolint:errcheck
}

// handleRJ starts the RJ workflow.
// workno may be empty, in which case the bot asks the user to type it.
func (b *BotServer) handleRJ(ctx context.Context, chatID int64, sess *session, tgUserID int64, workno string) {
	// Load asmr token.
	binding, err := b.cfg.BindingRepo.FindByTelegramID(ctx, tgUserID)
	if err != nil || binding.AsmrToken == "" {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"❌ No asmr.one token found.\n\nPlease run:\n<code>/asmr_bind &lt;your_jwt_token&gt;</code>", nil)
		return
	}

	if workno == "" {
		sess.state = stateRJWaitingID
		sess.rjAsmrToken = binding.AsmrToken
		b.store.set(chatID, sess)
		b.api.sendMessage(ctx, chatID, "Please enter the RJ work number (e.g. <code>RJ299717</code>):", nil) //nolint:errcheck
		return
	}

	b.fetchAndStartRJBrowse(ctx, chatID, sess, binding.AsmrToken, workno)
}

// handleRJIDInput processes text input when waiting for an RJ number.
func (b *BotServer) handleRJIDInput(ctx context.Context, chatID int64, sess *session, text string) {
	text = strings.TrimSpace(text)
	if !rjPattern.MatchString(text) {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"Invalid format. Please enter a valid RJ number (e.g. <code>RJ299717</code>):", nil)
		return
	}
	b.fetchAndStartRJBrowse(ctx, chatID, sess, sess.rjAsmrToken, text)
}

// fetchAndStartRJBrowse fetches work info + tracks and initialises the browse state.
func (b *BotServer) fetchAndStartRJBrowse(ctx context.Context, chatID int64, sess *session, token, workno string) {
	b.api.sendMessage(ctx, chatID, fmt.Sprintf("🔍 Looking up <code>%s</code>…", workno), nil) //nolint:errcheck

	client := asmrone.NewClient(token)

	info, err := client.GetWorkInfo(ctx, workno)
	if err != nil {
		b.api.sendMessage(ctx, chatID, "❌ "+err.Error(), nil) //nolint:errcheck
		b.store.reset(chatID)
		return
	}

	tracks, err := client.GetTracks(ctx, workno)
	if err != nil {
		b.api.sendMessage(ctx, chatID, "❌ "+err.Error(), nil) //nolint:errcheck
		b.store.reset(chatID)
		return
	}

	sess.state = stateRJBrowse
	sess.rjMode = true
	sess.rjWorkno = strings.ToUpper(workno)
	sess.rjWorkTitle = info.Title
	sess.rjAsmrToken = token
	sess.rjPath = nil
	sess.rjDirStack = nil
	sess.rjCurrentDir = tracks
	sess.rjSelectedURLs = make(map[string]asmrone.Track)
	sess.rjAllVTTs = asmrone.FlattenVTTs(tracks)
	sess.rjVTT = nil
	b.store.set(chatID, sess)

	text := b.rjBrowseText(sess)
	kb := b.rjBrowseKeyboard(sess)
	msgID, err := b.api.sendMessageGetID(ctx, chatID, text, kb)
	if err != nil {
		log.Printf("tgbot: send rj browse: %v", err)
		return
	}
	sess.rjMenuMsgID = msgID
	b.store.set(chatID, sess)
}

// --- Browse UI ---

func (b *BotServer) rjBrowseText(sess *session) string {
	title := sess.rjWorkTitle
	if title == "" {
		title = sess.rjWorkno
	}

	pathStr := "📂 " + sess.rjWorkno
	for _, p := range sess.rjPath {
		pathStr += " › " + p
	}

	vttCount := 0
	for _, t := range sess.rjCurrentDir {
		if t.IsVTT() {
			vttCount++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n<code>%s</code>\n\n", title, pathStr))
	sb.WriteString("Tap 🎵 to toggle audio selection. Tap 📁 to open a folder.\n")
	if vttCount > 0 {
		sb.WriteString(fmt.Sprintf("<i>(%d subtitle file(s) in this folder – selected after audio)</i>\n", vttCount))
	}
	if n := len(sess.rjSelectedURLs); n > 0 {
		sb.WriteString(fmt.Sprintf("\n<b>✅ Selected: %d audio file(s)</b>", n))
	}
	return sb.String()
}

func (b *BotServer) rjBrowseKeyboard(sess *session) *InlineKeyboardMarkup {
	items := asmrone.BrowseItems(sess.rjCurrentDir)
	var rows [][]InlineKeyboardButton

	for i, t := range items {
		var btn InlineKeyboardButton
		if t.IsFolder() {
			btn = InlineKeyboardButton{
				Text:         "📁 " + t.Title,
				CallbackData: fmt.Sprintf("rj:e:%d", i),
			}
		} else {
			sizeStr := ""
			if t.FileSize > 0 {
				sizeStr = fmt.Sprintf(" (%.1f MB)", float64(t.FileSize)/(1024*1024))
			}
			prefix := "🎵"
			if _, ok := sess.rjSelectedURLs[t.MediaDownloadURL]; ok {
				prefix = "✅"
			}
			btn = InlineKeyboardButton{
				Text:         fmt.Sprintf("%s %s%s", prefix, t.Title, sizeStr),
				CallbackData: fmt.Sprintf("rj:t:%d", i),
			}
		}
		rows = append(rows, []InlineKeyboardButton{btn})
	}

	// Navigation / action row.
	var navRow []InlineKeyboardButton
	if len(sess.rjDirStack) > 0 {
		navRow = append(navRow, InlineKeyboardButton{Text: "⬆️ Back", CallbackData: "rj:back"})
	}
	if len(sess.rjSelectedURLs) > 0 {
		navRow = append(navRow, InlineKeyboardButton{
			Text:         fmt.Sprintf("✔️ Done (%d)", len(sess.rjSelectedURLs)),
			CallbackData: "rj:done",
		})
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// refreshRJBrowse edits the current browse message in-place.
func (b *BotServer) refreshRJBrowse(ctx context.Context, chatID int64, sess *session) {
	if sess.rjMenuMsgID == 0 {
		return
	}
	text := b.rjBrowseText(sess)
	kb := b.rjBrowseKeyboard(sess)
	b.api.editMessageText(ctx, chatID, sess.rjMenuMsgID, text, kb) //nolint:errcheck
}

// --- Callback handlers for RJ browse ---

// handleRJToggle toggles selection of an audio file.
func (b *BotServer) handleRJToggle(ctx context.Context, chatID int64, sess *session, idxStr string) {
	if sess.state != stateRJBrowse {
		return
	}
	idx, items := b.rjParseIdx(sess, idxStr)
	if idx < 0 || !items[idx].IsAudio() {
		return
	}
	track := items[idx]
	if _, ok := sess.rjSelectedURLs[track.MediaDownloadURL]; ok {
		delete(sess.rjSelectedURLs, track.MediaDownloadURL)
	} else {
		sess.rjSelectedURLs[track.MediaDownloadURL] = track
	}
	b.store.set(chatID, sess)
	b.refreshRJBrowse(ctx, chatID, sess)
}

// handleRJEnter navigates into a folder.
func (b *BotServer) handleRJEnter(ctx context.Context, chatID int64, sess *session, idxStr string) {
	if sess.state != stateRJBrowse {
		return
	}
	idx, items := b.rjParseIdx(sess, idxStr)
	if idx < 0 || !items[idx].IsFolder() {
		return
	}
	folder := items[idx]
	sess.rjDirStack = append(sess.rjDirStack, dirLevel{title: folder.Title, tracks: sess.rjCurrentDir})
	sess.rjPath = append(sess.rjPath, folder.Title)
	sess.rjCurrentDir = folder.Children
	b.store.set(chatID, sess)
	b.refreshRJBrowse(ctx, chatID, sess)
}

// handleRJBack navigates up one folder level.
func (b *BotServer) handleRJBack(ctx context.Context, chatID int64, sess *session) {
	if sess.state != stateRJBrowse || len(sess.rjDirStack) == 0 {
		return
	}
	top := sess.rjDirStack[len(sess.rjDirStack)-1]
	sess.rjDirStack = sess.rjDirStack[:len(sess.rjDirStack)-1]
	sess.rjPath = sess.rjPath[:len(sess.rjPath)-1]
	sess.rjCurrentDir = top.tracks
	b.store.set(chatID, sess)
	b.refreshRJBrowse(ctx, chatID, sess)
}

// handleRJDoneAudio finalises audio selection and shows the VTT picker.
func (b *BotServer) handleRJDoneAudio(ctx context.Context, chatID int64, sess *session) {
	if sess.state != stateRJBrowse || len(sess.rjSelectedURLs) == 0 {
		return
	}

	// Preserve insertion order by iterating over selectedURLs map.
	sess.rjAudioFiles = make([]asmrone.Track, 0, len(sess.rjSelectedURLs))
	for _, t := range sess.rjSelectedURLs {
		sess.rjAudioFiles = append(sess.rjAudioFiles, t)
	}

	if len(sess.rjAllVTTs) == 0 {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"❌ No subtitle (.vtt) files found in this work.\n\n"+
				"Use /new to upload your audio and VTT files manually.", nil)
		b.store.reset(chatID)
		return
	}

	sess.state = stateRJSelectVTT
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, b.rjVTTText(sess), b.rjVTTKeyboard(sess)) //nolint:errcheck
}

// rjVTTText builds the VTT selection message.
func (b *BotServer) rjVTTText(sess *session) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n\n", sess.rjWorkTitle))
	sb.WriteString(fmt.Sprintf("Selected <b>%d</b> audio file(s).\n\n", len(sess.rjAudioFiles)))
	sb.WriteString("Now select the subtitle file (.vtt) to apply to all selected audio:")
	return sb.String()
}

// rjVTTKeyboard builds the VTT picker keyboard.
func (b *BotServer) rjVTTKeyboard(sess *session) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	for i, t := range sess.rjAllVTTs {
		sizeStr := ""
		if t.FileSize > 0 {
			sizeStr = fmt.Sprintf(" (%.1f KB)", float64(t.FileSize)/1024)
		}
		rows = append(rows, []InlineKeyboardButton{{
			Text:         fmt.Sprintf("📄 %s%s", t.Title, sizeStr),
			CallbackData: fmt.Sprintf("rj:v:%d", i),
		}})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleRJSelectVTTCallback handles the user picking a VTT file and moves to config.
func (b *BotServer) handleRJSelectVTTCallback(ctx context.Context, chatID int64, sess *session, idxStr string) {
	if sess.state != stateRJSelectVTT {
		return
	}
	var idx int
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil || idx < 0 || idx >= len(sess.rjAllVTTs) {
		return
	}
	vtt := sess.rjAllVTTs[idx]
	sess.rjVTT = &vtt

	// Populate audioName / vttName for the existing config summary display.
	sess.audioName = fmt.Sprintf("%d file(s) from %s", len(sess.rjAudioFiles), sess.rjWorkno)
	sess.vttName = vtt.Title

	sess.state = stateWaitingConfig
	sess.configStep = configStepProvider
	sess.cfg = domain.JobConfig{
		TTSVolume:   0.08,
		Concurrency: 3,
	}
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Subtitle: <b>%s</b>\n\nSelect a TTS provider:", vtt.Title),
		b.providerKeyboard())
}

// --- RJ job creation ---

// handleRJConfirm downloads audio + VTT from asmr.one and creates one job per audio file.
func (b *BotServer) handleRJConfirm(ctx context.Context, chatID int64, sess *session) {
	audioFiles := sess.rjAudioFiles
	vttTrack := sess.rjVTT
	token := sess.rjAsmrToken
	cfg := sess.cfg
	userID := sess.userID
	workno := sess.rjWorkno

	b.store.reset(chatID)
	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("⏳ Downloading %d audio file(s) and subtitle from asmr.one…", len(audioFiles)), nil)

	go func() {
		bgCtx := context.Background()

		// Download VTT once; all jobs share the same key.
		vttKey, err := b.downloadFromAsmrOne(bgCtx, token, vttTrack.MediaDownloadURL, userID, vttTrack.Title)
		if err != nil {
			log.Printf("tgbot: rj download vtt: %v", err)
			b.api.sendMessage(bgCtx, chatID, "❌ Failed to download subtitle: "+err.Error(), nil) //nolint:errcheck
			return
		}

		var jobIDs []string
		for _, audioTrack := range audioFiles {
			audioKey, err := b.downloadFromAsmrOne(bgCtx, token, audioTrack.MediaDownloadURL, userID, audioTrack.Title)
			if err != nil {
				log.Printf("tgbot: rj download audio %s: %v", audioTrack.Title, err)
				b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
					fmt.Sprintf("❌ Failed to download %s: %s", audioTrack.Title, err.Error()), nil)
				continue
			}

			job, err := b.cfg.JobSvc.CreateJob(bgCtx, userID, audioKey, vttKey, audioTrack.Title, vttTrack.Title, cfg)
			if err != nil {
				log.Printf("tgbot: rj create job for %s: %v", audioTrack.Title, err)
				b.cfg.Storage.Delete(bgCtx, audioKey) //nolint:errcheck
				b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
					fmt.Sprintf("❌ Failed to queue job for %s: %s", audioTrack.Title, err.Error()), nil)
				continue
			}
			jobIDs = append(jobIDs, job.ID)
			go b.notifier.watch(bgCtx, chatID, job.ID)
		}

		if len(jobIDs) == 0 {
			b.cfg.Storage.Delete(bgCtx, vttKey) //nolint:errcheck
			return
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("✅ Queued <b>%d</b> job(s) from <code>%s</code>!\n\n", len(jobIDs), workno))
		for i, id := range jobIDs {
			sb.WriteString(fmt.Sprintf("%d. <code>%s</code>\n", i+1, id))
		}
		sb.WriteString("\nI'll notify you as each one finishes.")
		b.api.sendMessage(bgCtx, chatID, sb.String(), nil) //nolint:errcheck
	}()
}

// downloadFromAsmrOne downloads a file from an asmr.one URL and uploads it to R2.
// Returns the R2 object key.
func (b *BotServer) downloadFromAsmrOne(ctx context.Context, token, downloadURL, userID, fileName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asmr.one returned HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "rj-*-"+sanitizeFileName(fileName))
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
	return key, nil
}

// --- Helpers ---

// rjParseIdx parses an index string and returns (idx, browseItems).
// Returns (-1, nil) on parse error or out-of-range.
func (b *BotServer) rjParseIdx(sess *session, idxStr string) (int, []asmrone.Track) {
	var idx int
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
		return -1, nil
	}
	items := asmrone.BrowseItems(sess.rjCurrentDir)
	if idx < 0 || idx >= len(items) {
		return -1, nil
	}
	return idx, items
}
