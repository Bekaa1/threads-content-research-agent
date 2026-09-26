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
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return errors.New("Telegram returned an invalid response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		return fmt.Errorf("Telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
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
