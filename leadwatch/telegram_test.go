package leadwatch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTelegramFollowsServerConfirmedGroupMigrationOnce(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: telegramTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls == 1 {
			if body["chat_id"] != "-123" {
				t.Fatal("unexpected original inbox")
			}
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"ok":false,"parameters":{"migrate_to_chat_id":-100123}}`))}, nil
		}
		if body["chat_id"] != "-100123" {
			t.Fatal("migration target missing")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	n, _ := NewTelegramNotifier("test-token", "-123", client)
	if err := n.Send(context.Background(), Lead{}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
func TestTelegramErrorsAreCategorizedAndRedacted(t *testing.T) {
	cases := []struct{ description, want string }{{"Bad Request: chat not found", "chat_not_found"}, {"Bad Request: can't parse entities", "invalid_html"}, {"secret-token private details", "request_rejected"}}
	for _, tc := range cases {
		body, _ := json.Marshal(map[string]any{"ok": false, "description": tc.description})
		n, _ := NewTelegramNotifier("secret-token", "-123", &http.Client{Transport: telegramTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})})
		err := n.Send(context.Background(), Lead{})
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("unsafe or incorrect error: %v", err)
		}
	}
}
func TestTelegramMigrationCannotLoop(t *testing.T) {
	calls := 0
	n, _ := NewTelegramNotifier("test", "-123", &http.Client{Transport: telegramTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"migrated","parameters":{"migrate_to_chat_id":-100123}}`))}, nil
	})})
	if err := n.Send(context.Background(), Lead{}); err == nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestTelegramLeadIncludesOpenAndCopyButtons(t *testing.T) {
	client := &http.Client{Transport: telegramTransport(func(r *http.Request) (*http.Response, error) {
		var body struct {
			ReplyMarkup struct {
				InlineKeyboard [][]struct {
					Text     string `json:"text"`
					URL      string `json:"url"`
					CopyText *struct {
						Text string `json:"text"`
					} `json:"copy_text"`
				} `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		buttons := body.ReplyMarkup.InlineKeyboard
		if len(buttons) != 2 || len(buttons[0]) != 2 || len(buttons[1]) != 2 {
			t.Fatalf("unexpected keyboard: %+v", buttons)
		}
		if buttons[0][0].URL != "https://www.threads.com/@buyer/post/AbCdEf" || buttons[0][1].URL != "https://www.threads.com/@buyer" {
			t.Fatalf("unexpected links: %+v", buttons[0])
		}
		if buttons[1][0].CopyText == nil || buttons[1][0].CopyText.Text != "Hello in DM" || buttons[1][1].CopyText == nil || buttons[1][1].CopyText.Text != "Public reply" {
			t.Fatalf("unexpected copy actions: %+v", buttons[1])
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	n, _ := NewTelegramNotifier("test", "-123", client)
	lead := Lead{
		SearchResult: threads.SearchResult{Username: "buyer", Permalink: "https://www.threads.com/@buyer/post/AbCdEf"},
		Assessment:   Assessment{DMDraft: "Hello in DM", CommentDraft: "Public reply"},
	}
	if err := n.Send(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
}
