package leadwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type TelegramNotifier struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegramNotifier(token, chatID string, client *http.Client) (*TelegramNotifier, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return nil, errors.New("Telegram bot token and chat id are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &TelegramNotifier{token: strings.TrimSpace(token), chatID: strings.TrimSpace(chatID), client: client}, nil
}

func (n *TelegramNotifier) Send(ctx context.Context, text string) error {
	return n.send(ctx, text, true)
}

func (n *TelegramNotifier) send(ctx context.Context, text string, allowMigration bool) error {
	payload, err := json.Marshal(map[string]any{"chat_id": n.chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true})
	if err != nil {
		return errors.New("could not encode Telegram message")
	}
	url := "https://api.telegram.org/bot" + n.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return errors.New("could not create Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return errors.New("Telegram request failed")
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Parameters  struct {
			MigrateToChatID int64 `json:"migrate_to_chat_id"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return errors.New("Telegram returned an invalid response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		// A Telegram-confirmed group->supergroup migration is the same inbox,
		// not a new recipient. Follow once; leave the lead pending on any failure.
		if resp.StatusCode == http.StatusBadRequest && allowMigration && result.Parameters.MigrateToChatID < 0 {
			n.chatID = strconv.FormatInt(result.Parameters.MigrateToChatID, 10)
			return n.send(ctx, text, false)
		}
		return fmt.Errorf("Telegram returned HTTP %d (%s)", resp.StatusCode, telegramErrorCategory(resp.StatusCode, result.Description))
	}
	return nil
}

// Never log raw API responses: return only an allowlisted diagnostic category.
func telegramErrorCategory(code int, description string) string {
	d := strings.ToLower(description)
	switch {
	case strings.Contains(d, "chat not found"):
		return "chat_not_found"
	case strings.Contains(d, "bot was kicked"):
		return "bot_removed"
	case strings.Contains(d, "not enough rights"), strings.Contains(d, "chat_write_forbidden"):
		return "missing_write_permission"
	case strings.Contains(d, "parse entities"):
		return "invalid_html"
	case strings.Contains(d, "message is too long"):
		return "message_too_long"
	case strings.Contains(d, "migrat"):
		return "chat_migrated"
	case code == 401:
		return "invalid_bot_token"
	case code == 403:
		return "forbidden"
	case code == 429:
		return "rate_limited"
	default:
		return "request_rejected"
	}
}

func formatTelegramLead(lead Lead) string {
	username := strings.TrimSpace(lead.Username)
	if username == "" {
		username = "unknown"
	}
	postText := strings.TrimSpace(lead.Text)
	if len([]rune(postText)) > 900 {
		postText = string([]rune(postText)[:900]) + "…"
	}
	lines := []string{
		"<b>Новый лид из Threads</b> · " + html.EscapeString(lead.Category) + " · " + fmt.Sprintf("%d/100", lead.Score),
		"Автор: @" + html.EscapeString(username),
	}
	if lead.Query != "" {
		lines = append(lines, "Запрос: "+html.EscapeString(lead.Query))
	}
	if lead.Source != "" {
		lines = append(lines, "Источник: "+html.EscapeString(lead.Source))
	}
	if !lead.Timestamp.IsZero() {
		lines = append(lines, "Дата поста (UTC): "+lead.Timestamp.UTC().Format("2006-01-02 15:04"))
	}
	if strings.HasPrefix(lead.Permalink, "https://www.threads.com/") || strings.HasPrefix(lead.Permalink, "https://threads.com/") {
		lines = append(lines, "<a href=\""+html.EscapeString(lead.Permalink)+"\">Открыть пост</a>")
	}
	lines = append(lines, "", html.EscapeString(postText))
	if reason := strings.TrimSpace(lead.Reason); reason != "" {
		lines = append(lines, "", "Почему подходит: "+html.EscapeString(reason))
	}
	if draft := strings.TrimSpace(lead.Draft); draft != "" {
		lines = append(lines, "", "<b>Черновик (отправить вручную):</b>", html.EscapeString(draft))
	}
	message := strings.Join(lines, "\n")
	if len([]rune(message)) > 3900 {
		message = string([]rune(message)[:3900]) + "…"
	}
	return message
}
