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
func (b *BotServer) handleAsmrBind(ctx context.Context, chatID int64, tgUserID int64, userID, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			"Usage: <code>/asmr_bind &lt;your_jwt_token&gt;</code>\n\n"+
				"Paste your asmr.one JWT token to enable RJ-based jobs.\n"+
				"The token is valid for ~1 year.", nil)
		return
	}
	if err := b.cfg.BindingRepo.SaveAsmrToken(ctx, tgUserID, userID, token); err != nil {
		log.Printf("tgbot: save asmr token for tg %d: %v", tgUserID, err)
		b.api.sendMessage(ctx, chatID, "❌ Failed to save token, please try again.", nil) //nolint:errcheck
		return
	}
	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		"✅ asmr.one token saved.\n\nUse /rj <code>RJxxxxxx</code> to start a job from asmr.one.", nil)
}

// handleAsmrUnbind removes the user's stored asmr.one JWT token.
func (b *BotServer) handleAsmrUnbind(ctx context.Context, chatID int64, tgUserID int64) {
	if err := b.cfg.BindingRepo.ClearAsmrToken(ctx, tgUserID); err != nil {
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
	if !info.HasSubtitle {
		b.api.sendMessage(ctx, chatID, //nolint:errcheck
			fmt.Sprintf("❌ <code>%s</code> does not have subtitle files and cannot be used for translation.", workno), nil)
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
	sess.rjSelectedURLs = make(map[string]asmrone.AudioVTTPair)
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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n<code>%s</code>\n\n", title, pathStr))
	sb.WriteString("Tap 🎵 to toggle audio selection. Tap 📁 to open a folder.\n")
	sb.WriteString("<i>(Only audio files with paired subtitles are shown.)</i>\n")
	if n := len(sess.rjSelectedURLs); n > 0 {
		sb.WriteString(fmt.Sprintf("\n<b>✅ Selected: %d audio file(s)</b>", n))
	}
	return sb.String()
}

func (b *BotServer) rjBrowseKeyboard(sess *session) *InlineKeyboardMarkup {
	items := asmrone.SubtitledBrowseItems(sess.rjCurrentDir)
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

	// "Select All" row – only shown when there is at least one subtitled audio in this dir.
	dirAudios := asmrone.SubtitledAudioInDir(sess.rjCurrentDir)
	if len(dirAudios) > 0 {
		allSelected := true
		for _, t := range dirAudios {
			if _, ok := sess.rjSelectedURLs[t.MediaDownloadURL]; !ok {
				allSelected = false
				break
			}
		}
		selectAllText := fmt.Sprintf("☑️ Select All (%d)", len(dirAudios))
		if allSelected {
			selectAllText = fmt.Sprintf("✅ Deselect All (%d)", len(dirAudios))
		}
		rows = append(rows, []InlineKeyboardButton{{
			Text:         selectAllText,
			CallbackData: "rj:all",
		}})
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
		vtt := track.FindVTTPeer(sess.rjCurrentDir)
		if vtt == nil {
			return
		}
		sess.rjSelectedURLs[track.MediaDownloadURL] = asmrone.AudioVTTPair{Audio: track, VTT: *vtt}
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

// handleRJDoneAudio finalises audio selection and moves directly to config.
// Each selected audio already has its paired VTT determined at selection time.
func (b *BotServer) handleRJDoneAudio(ctx context.Context, chatID int64, sess *session) {
	if sess.state != stateRJBrowse || len(sess.rjSelectedURLs) == 0 {
		return
	}

	sess.audioName = fmt.Sprintf("%d file(s) from %s", len(sess.rjSelectedURLs), sess.rjWorkno)
	sess.vttName = "auto-paired"

	sess.state = stateWaitingConfig
	sess.configStep = configStepProvider
	sess.cfg = domain.JobConfig{
		TTSVolume:   0.08,
		Concurrency: 3,
	}
	b.store.set(chatID, sess)

	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("✅ Selected <b>%d</b> audio file(s) with paired subtitles.\n\nSelect a TTS provider:",
			len(sess.rjSelectedURLs)),
		b.providerKeyboard())
}

// handleRJSelectAll toggles selection of all subtitled audio files in the
// current directory. If every file is already selected, they are all deselected.
func (b *BotServer) handleRJSelectAll(ctx context.Context, chatID int64, sess *session) {
	if sess.state != stateRJBrowse {
		return
	}
	audios := asmrone.SubtitledAudioInDir(sess.rjCurrentDir)
	if len(audios) == 0 {
		return
	}
	allSelected := true
	for _, t := range audios {
		if _, ok := sess.rjSelectedURLs[t.MediaDownloadURL]; !ok {
			allSelected = false
			break
		}
	}
	if allSelected {
		for _, t := range audios {
			delete(sess.rjSelectedURLs, t.MediaDownloadURL)
		}
	} else {
		for _, t := range audios {
			vtt := t.FindVTTPeer(sess.rjCurrentDir)
			if vtt != nil {
				sess.rjSelectedURLs[t.MediaDownloadURL] = asmrone.AudioVTTPair{Audio: t, VTT: *vtt}
			}
		}
	}
	b.store.set(chatID, sess)
	b.refreshRJBrowse(ctx, chatID, sess)
}

// --- RJ job creation ---

// handleRJConfirm downloads audio + VTT from asmr.one and creates one job per audio file.
func (b *BotServer) handleRJConfirm(ctx context.Context, chatID int64, sess *session) {
	pairs := make([]asmrone.AudioVTTPair, 0, len(sess.rjSelectedURLs))
	for _, p := range sess.rjSelectedURLs {
		pairs = append(pairs, p)
	}
	token := sess.rjAsmrToken
	cfg := sess.cfg
	userID := sess.userID
	workno := sess.rjWorkno

	b.store.reset(chatID)
	b.api.sendMessage(ctx, chatID, //nolint:errcheck
		fmt.Sprintf("⏳ Downloading %d audio file(s) and subtitles from asmr.one…", len(pairs)), nil)

	go func() {
		bgCtx := context.Background()

		var jobIDs []string
		for _, pair := range pairs {
			vttKey, err := b.downloadFromAsmrOne(bgCtx, token, pair.VTT.MediaDownloadURL, userID, pair.VTT.Title)
			if err != nil {
				log.Printf("tgbot: rj download vtt %s: %v", pair.VTT.Title, err)
				b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
					fmt.Sprintf("❌ Failed to download subtitle for %s: %s", pair.Audio.Title, err.Error()), nil)
				continue
			}

			audioKey, err := b.downloadFromAsmrOne(bgCtx, token, pair.Audio.MediaDownloadURL, userID, pair.Audio.Title)
			if err != nil {
				log.Printf("tgbot: rj download audio %s: %v", pair.Audio.Title, err)
				b.cfg.Storage.Delete(bgCtx, vttKey) //nolint:errcheck
				b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
					fmt.Sprintf("❌ Failed to download %s: %s", pair.Audio.Title, err.Error()), nil)
				continue
			}

			job, err := b.cfg.JobSvc.CreateJob(bgCtx, userID, audioKey, vttKey, pair.Audio.Title, pair.VTT.Title, cfg)
			if err != nil {
				log.Printf("tgbot: rj create job for %s: %v", pair.Audio.Title, err)
				b.cfg.Storage.Delete(bgCtx, audioKey) //nolint:errcheck
				b.cfg.Storage.Delete(bgCtx, vttKey)   //nolint:errcheck
				b.api.sendMessage(bgCtx, chatID, //nolint:errcheck
					fmt.Sprintf("❌ Failed to queue job for %s: %s", pair.Audio.Title, err.Error()), nil)
				continue
			}
			jobIDs = append(jobIDs, job.ID)
			go b.notifier.watch(bgCtx, chatID, job.ID)
		}

		if len(jobIDs) == 0 {
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
	items := asmrone.SubtitledBrowseItems(sess.rjCurrentDir)
	if idx < 0 || idx >= len(items) {
		return -1, nil
	}
	return idx, items
}
