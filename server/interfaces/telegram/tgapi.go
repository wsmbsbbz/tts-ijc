package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"
)

// --- Telegram Bot API types ---

// Update is an incoming update from Telegram.
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// Message is a Telegram message.
type Message struct {
	MessageID int       `json:"message_id"`
	From      *TGUser   `json:"from"`
	Chat      Chat      `json:"chat"`
	Text      string    `json:"text"`
	Document  *Document `json:"document"`
	Audio     *Audio    `json:"audio"`
	Voice     *Voice    `json:"voice"`
	Video     *Video    `json:"video"`
}

// TGUser is a Telegram user or bot.
type TGUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// Chat is the Telegram chat the message was sent in.
type Chat struct {
	ID int64 `json:"id"`
}

// Document is a Telegram file sent as a document.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	MIMEType string `json:"mime_type"`
}

// Audio is an audio file.
type Audio struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	MIMEType string `json:"mime_type"`
}

// Voice is a voice message.
type Voice struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	MIMEType string `json:"mime_type"`
}

// Video is a video file.
type Video struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	MIMEType string `json:"mime_type"`
}

// CallbackQuery is fired when a user taps an inline keyboard button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *TGUser  `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// TGFile contains metadata needed to download a file from Telegram.
type TGFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

// InlineKeyboardMarkup is an inline keyboard attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton is a single button in an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// --- API client ---

type tgAPI struct {
	token   string
	baseURL string
	client  *http.Client
}

func newTGAPI(token, baseURL string) *tgAPI {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &tgAPI{
		token:   token,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (a *tgAPI) methodURL(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", a.baseURL, a.token, method)
}

// FileDownloadURL returns the URL to download a file from Telegram.
// Only used in standard (non-local) mode; in local mode file_path is an
// absolute filesystem path and files are read directly without HTTP.
func (a *tgAPI) FileDownloadURL(filePath string) string {
	return fmt.Sprintf("%s/file/bot%s/%s", a.baseURL, a.token, filePath)
}

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

func (a *tgAPI) call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.methodURL(method), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var ar apiResp
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !ar.OK {
		return nil, fmt.Errorf("telegram api [%d]: %s", ar.ErrorCode, ar.Description)
	}
	return ar.Result, nil
}

func (a *tgAPI) getUpdates(ctx context.Context, offset, timeout int) ([]Update, error) {
	result, err := a.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         timeout,
		"allowed_updates": []string{"message", "callback_query"},
	})
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, fmt.Errorf("parse updates: %w", err)
	}
	return updates, nil
}

func (a *tgAPI) sendMessage(ctx context.Context, chatID int64, text string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	_, err := a.call(ctx, "sendMessage", payload)
	return err
}

func (a *tgAPI) answerCallbackQuery(ctx context.Context, callbackID, text string) error {
	_, err := a.call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
	})
	return err
}

func (a *tgAPI) getFile(ctx context.Context, fileID string) (TGFile, error) {
	result, err := a.call(ctx, "getFile", map[string]any{"file_id": fileID})
	if err != nil {
		return TGFile{}, err
	}
	var f TGFile
	if err := json.Unmarshal(result, &f); err != nil {
		return TGFile{}, err
	}
	return f, nil
}

// sendDocument sends a file to chatID using a URL (Telegram fetches it directly).
func (a *tgAPI) sendDocument(ctx context.Context, chatID int64, documentURL, caption string) error {
	_, err := a.call(ctx, "sendDocument", map[string]any{
		"chat_id":    chatID,
		"document":   documentURL,
		"caption":    caption,
		"parse_mode": "HTML",
	})
	return err
}

// sendDocumentMultipart uploads a local file to chatID as a document via
// multipart form-data. Unlike sendDocument (which asks Telegram to fetch a
// URL), this streams the bytes directly — required for the local Bot API
// server which does not proxy outbound R2 fetches.
func (a *tgAPI) sendDocumentMultipart(ctx context.Context, chatID int64, localPath, filename, caption string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
		_ = mw.WriteField("caption", caption)
		_ = mw.WriteField("parse_mode", "HTML")
		part, err := mw.CreateFormFile("document", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		mw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.methodURL("sendDocument"), pr)
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	// No client-level timeout — rely on ctx for cancellation.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var ar apiResp
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if !ar.OK {
		return fmt.Errorf("telegram api [%d]: %s", ar.ErrorCode, ar.Description)
	}
	return nil
}

// localAudio is a local file to be sent as part of an audio media group.
type localAudio struct {
	path     string
	filename string
	caption  string // HTML caption; only used on the first item
}

// sendAudioGroupMultipart uploads 2–10 local audio files as a single Telegram
// media group (audio album). Each file is referenced via attach:// in the
// media JSON and uploaded as a separate multipart part.
func (a *tgAPI) sendAudioGroupMultipart(ctx context.Context, chatID int64, audios []localAudio) error {
	if len(audios) < 2 || len(audios) > 10 {
		return fmt.Errorf("sendAudioGroupMultipart: need 2–10 items, got %d", len(audios))
	}

	// Open all files first so we can fail early.
	files := make([]*os.File, len(audios))
	for i, a := range audios {
		f, err := os.Open(a.path)
		if err != nil {
			// Close any already-opened files.
			for j := 0; j < i; j++ {
				files[j].Close()
			}
			return fmt.Errorf("open audio %d: %w", i, err)
		}
		files[i] = f
	}
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()

	// Build the media JSON array.
	type inputMediaAudio struct {
		Type      string `json:"type"`
		Media     string `json:"media"`
		Caption   string `json:"caption,omitempty"`
		ParseMode string `json:"parse_mode,omitempty"`
	}
	mediaItems := make([]inputMediaAudio, len(audios))
	for i, a := range audios {
		item := inputMediaAudio{
			Type:  "audio",
			Media: fmt.Sprintf("attach://audio_%d", i),
		}
		if a.caption != "" {
			item.Caption = a.caption
			item.ParseMode = "HTML"
		}
		mediaItems[i] = item
	}
	mediaJSON, err := json.Marshal(mediaItems)
	if err != nil {
		return fmt.Errorf("marshal media: %w", err)
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
		_ = mw.WriteField("media", string(mediaJSON))
		for i, audio := range audios {
			fieldName := fmt.Sprintf("audio_%d", i)
			part, err := mw.CreateFormFile(fieldName, audio.filename)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("create form file %d: %w", i, err))
				return
			}
			if _, err := io.Copy(part, files[i]); err != nil {
				pw.CloseWithError(fmt.Errorf("copy file %d: %w", i, err))
				return
			}
		}
		mw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.methodURL("sendMediaGroup"), pr)
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var ar apiResp
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if !ar.OK {
		return fmt.Errorf("telegram api [%d]: %s", ar.ErrorCode, ar.Description)
	}
	return nil
}

// sendPhoto sends a photo by URL with an HTML caption and returns the message ID.
func (a *tgAPI) sendPhoto(ctx context.Context, chatID int64, photoURL, caption string, keyboard *InlineKeyboardMarkup) (int, error) {
	payload := map[string]any{
		"chat_id":    chatID,
		"photo":      photoURL,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	result, err := a.call(ctx, "sendPhoto", payload)
	if err != nil {
		return 0, err
	}
	var msg Message
	if err := json.Unmarshal(result, &msg); err != nil {
		return 0, fmt.Errorf("parse sent photo: %w", err)
	}
	return msg.MessageID, nil
}

// editMessageCaption edits the caption of a photo/document message.
func (a *tgAPI) editMessageCaption(ctx context.Context, chatID int64, messageID int, caption string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"caption":    caption,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	_, err := a.call(ctx, "editMessageCaption", payload)
	return err
}

// deleteMessage deletes a message from the chat.
func (a *tgAPI) deleteMessage(ctx context.Context, chatID int64, messageID int) error {
	_, err := a.call(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	})
	return err
}

// BotCommand represents a single slash command shown in the Telegram menu.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// editMessageText edits the text (and optionally the keyboard) of an existing message.
func (a *tgAPI) editMessageText(ctx context.Context, chatID int64, messageID int, text string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	_, err := a.call(ctx, "editMessageText", payload)
	return err
}

// sendMessageGetID sends a message and returns the resulting message ID.
func (a *tgAPI) sendMessageGetID(ctx context.Context, chatID int64, text string, keyboard *InlineKeyboardMarkup) (int, error) {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	result, err := a.call(ctx, "sendMessage", payload)
	if err != nil {
		return 0, err
	}
	var msg Message
	if err := json.Unmarshal(result, &msg); err != nil {
		return 0, fmt.Errorf("parse sent message: %w", err)
	}
	return msg.MessageID, nil
}

// setMyCommands registers the bot's command list with Telegram so users see
// slash-command suggestions when they type "/".
func (a *tgAPI) setMyCommands(ctx context.Context, commands []BotCommand) error {
	_, err := a.call(ctx, "setMyCommands", map[string]any{
		"commands": commands,
	})
	return err
}
