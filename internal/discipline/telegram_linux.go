package discipline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type TelegramConfig struct {
	TokenFile string
	ChatID    int64
	Users     []int64
}

type telegram struct {
	token, base string
	chat        int64
	users       map[int64]bool
	client      *http.Client
}

var errTelegram = errors.New("Telegram unavailable")
var botToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{20,}$`)
var telegramFile = regexp.MustCompile(`^(photos|documents)/[A-Za-z0-9_.-]+$`)

func newTelegram(cfg TelegramConfig) (*telegram, error) {
	if cfg.ChatID == 0 || len(cfg.Users) == 0 || !filepath.IsAbs(cfg.TokenFile) || strings.HasPrefix(filepath.Clean(cfg.TokenFile), "/nix/store/") {
		return nil, errors.New("review requires a private token file, allowed chat and allowed users")
	}
	info, err := os.Lstat(cfg.TokenFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("Telegram token file must be private and regular")
	}
	data, err := readRegular(cfg.TokenFile, 512)
	if err != nil {
		return nil, errors.New("cannot read Telegram token file")
	}
	token := strings.TrimSpace(string(data))
	if !botToken.MatchString(token) {
		return nil, errors.New("invalid Telegram token format")
	}
	users := map[int64]bool{}
	for _, id := range cfg.Users {
		if id <= 0 {
			return nil, errors.New("allowed Telegram user IDs must be positive")
		}
		users[id] = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Telegram redirects are forbidden") }}
	return &telegram{token: token, base: "https://api.telegram.org", chat: cfg.ChatID, users: users, client: client}, nil
}

func (t *telegram) request(ctx context.Context, method, contentType string, body io.Reader, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/"+method, body)
	if err != nil {
		return errTelegram
	}
	req.Header.Set("Content-Type", contentType)
	response, err := t.client.Do(req)
	// Transport errors contain credential-bearing URLs; never return them.
	if err != nil {
		return fmt.Errorf("%w: %s request failed", errTelegram, method)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return fmt.Errorf("%w: response read failed", errTelegram)
	}
	var envelope struct {
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if json.Unmarshal(data, &envelope) != nil || response.StatusCode != 200 || !envelope.OK {
		return fmt.Errorf("%w: %s HTTP %d API %d", errTelegram, method, response.StatusCode, envelope.ErrorCode)
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		return fmt.Errorf("%w: invalid %s response", errTelegram, method)
	}
	return nil
}
func (t *telegram) call(ctx context.Context, method string, payload, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return t.request(ctx, method, "application/json", bytes.NewReader(body), result)
}

type tgUser struct {
	ID int64 `json:"id"`
}
type tgChat struct {
	ID int64 `json:"id"`
}
type tgFile struct {
	ID     string `json:"file_id"`
	Size   int64  `json:"file_size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type tgMessage struct {
	ID       int64      `json:"message_id"`
	Chat     tgChat     `json:"chat"`
	From     tgUser     `json:"from"`
	Text     string     `json:"text"`
	Reply    *tgMessage `json:"reply_to_message"`
	Photos   []tgFile   `json:"photo"`
	Document *tgFile    `json:"document"`
}
type tgCallback struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}
type tgUpdate struct {
	ID       int64       `json:"update_id"`
	Message  *tgMessage  `json:"message"`
	Callback *tgCallback `json:"callback_query"`
}

func (u tgUpdate) receipt() updateReceipt {
	key := "ignored:" + strconv.FormatInt(u.ID, 10)
	if u.Message != nil && u.Callback != nil {
		return updateReceipt{ID: u.ID}
	}
	if u.Callback != nil && u.Callback.ID != "" {
		key = "callback:" + u.Callback.ID
	} else if u.Message != nil && u.Message.ID > 0 && u.Message.Chat.ID != 0 {
		key = fmt.Sprintf("message:%d:%d", u.Message.Chat.ID, u.Message.ID)
	}
	return updateReceipt{ID: u.ID, Key: key}
}

func (t *telegram) updates(ctx context.Context, offset int64, wait int) ([]tgUpdate, error) {
	var updates []tgUpdate
	err := t.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": wait, "limit": 20, "allowed_updates": []string{"message", "callback_query"}}, &updates)
	if err == nil {
		for _, update := range updates {
			if !update.receipt().valid() {
				return nil, fmt.Errorf("%w: invalid update identity", errTelegram)
			}
		}
	}
	return updates, err
}
func (t *telegram) answer(ctx context.Context, id, text string) {
	if err := t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil); err != nil {
		log.Printf("callback acknowledgement: %v", err)
	}
}

func (t *telegram) upload(ctx context.Context, r Review, image []byte) (int64, error) {
	caption := "Ripped, awaiting review.\nJob " + r.JobID + "\n" + r.Title + "\nReply with an HTTPS URL, photo or JPEG/PNG/WebP file to replace the cover."
	var message tgMessage
	if len(image) == 0 {
		err := t.call(ctx, "sendMessage", map[string]any{"chat_id": t.chat, "text": "No usable cover.\n" + caption, "link_preview_options": map[string]bool{"is_disabled": true}}, &message)
		if err != nil {
			return 0, err
		}
	} else {
		caption += "\nChoose a category below to approve this image."
		if r.Error != "" {
			caption = "Last replacement failed; this is still the previous candidate.\n" + caption
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(image))
		if err != nil {
			return 0, errors.New("stored candidate is not a supported JPEG")
		}
		field, method := "photo", "sendPhoto"
		if config.Width > 20*config.Height || config.Height > 20*config.Width {
			field, method = "document", "sendDocument"
			caption += "\nOpen the attached JPEG to inspect this wide or tall cover."
		}
		if r.Source == "" {
			caption = "Uploaded image; no public source URL.\n" + caption
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		fields := map[string]string{"chat_id": strconv.FormatInt(t.chat, 10), "caption": caption, "reply_markup": `{"inline_keyboard":[[{"text":"Approve Owned","callback_data":"Owned"},{"text":"Approve Loaned","callback_data":"Loaned"}]]}`}
		if r.Source != "" {
			fields["caption"] = "Source\n" + caption
			entities, _ := json.Marshal([]map[string]any{{"type": "text_link", "offset": 0, "length": 6, "url": r.Source}})
			fields["caption_entities"] = string(entities)
		}
		for key, value := range fields {
			if err := form.WriteField(key, value); err != nil {
				return 0, err
			}
		}
		part, err := form.CreateFormFile(field, "cover.jpg")
		if err != nil {
			return 0, err
		}
		if _, err := part.Write(image); err != nil {
			return 0, err
		}
		if err := form.Close(); err != nil {
			return 0, err
		}
		if err := t.request(ctx, method, form.FormDataContentType(), &body, &message); err != nil {
			return 0, err
		}
	}
	if message.ID <= 0 || message.Chat.ID != t.chat {
		return 0, fmt.Errorf("%w: invalid sent-message identity", errTelegram)
	}
	return message.ID, nil
}

func (t *telegram) image(ctx context.Context, id string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var file struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := t.call(ctx, "getFile", map[string]string{"file_id": id}, &file); err != nil {
		return nil, err
	}
	if !telegramFile.MatchString(file.Path) || path.Clean(file.Path) != file.Path || file.Size > imageLimit {
		return nil, errors.New("invalid or oversized Telegram image")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+"/file/bot"+t.token+"/"+file.Path, nil)
	if err != nil {
		return nil, errTelegram
	}
	response, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: image download failed", errTelegram)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength > imageLimit {
		return nil, errors.New("Telegram image unavailable or oversized")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, imageLimit+1))
	if err != nil || len(data) > imageLimit {
		return nil, errors.New("Telegram image read failed or exceeded byte limit")
	}
	return data, nil
}
