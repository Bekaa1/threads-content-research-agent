package leadwatch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
	if err := n.Send(context.Background(), "<b>Test</b>"); err != nil {
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
		err := n.Send(context.Background(), "Test")
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
	if err := n.Send(context.Background(), "Test"); err == nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
