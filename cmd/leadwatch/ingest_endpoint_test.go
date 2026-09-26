package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type browserTestStore struct {
	posts []threads.SearchResult
	fail  bool
}

func (s *browserTestStore) InsertNew(_ context.Context, posts []threads.SearchResult) ([]threads.SearchResult, error) {
	if s.fail {
		return nil, errors.New("secret database connection")
	}
	var added []threads.SearchResult
	for _, p := range posts {
		seen := false
		for _, old := range s.posts {
			if old.ID == p.ID {
				seen = true
			}
		}
		if !seen {
			s.posts = append(s.posts, p)
			added = append(added, p)
		}
	}
	return added, nil
}
func validBrowserBatch() browserBatch {
	return browserBatch{Query: "need a website", SourceURL: "https://www.threads.com/search?q=need+a+website&filter=recent", Posts: []browserPost{{Permalink: "https://www.threads.com/@buyer/post/Ddv8MUkGt1S", Text: "We need a website for our shop. Looking for a developer.", PostedAt: time.Now().Add(-time.Hour)}}}
}
func TestIngestValidationAndDedupe(t *testing.T) {
	key := strings.Repeat("a", 64)
	store := &browserTestStore{}
	triggered := 0
	handler := ingestPostsHandler(key, store, func() { triggered++ }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(batch browserBatch, auth string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(batch)
		req := httptest.NewRequest("POST", "/ingest/posts", strings.NewReader(string(data)))
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, auth := range []string{"", "Bearer wrong"} {
		if w := call(validBrowserBatch(), auth); w.Code != 401 {
			t.Fatalf("auth got %d", w.Code)
		}
	}
	batch := validBrowserBatch()
	if w := call(batch, "Bearer "+key); w.Code != 200 {
		t.Fatalf("valid batch: %s", w.Body.String())
	}
	if len(store.posts) != 1 || triggered != 1 {
		t.Fatal("batch not saved/triggered")
	}
	p := store.posts[0]
	if p.ID != "3994676124005883218" || p.Username != "buyer" || p.Source != threads.SearchSourceBrowser || !p.VerifiedSearch() {
		t.Fatalf("incorrect provenance: %+v", p)
	}
	if w := call(batch, "Bearer "+key); w.Code != 200 || triggered != 1 {
		t.Fatal("duplicate should not trigger")
	}
	for _, source := range []string{"https://evil.example/search?q=need+a+website&filter=recent", "https://www.threads.com/search?q=unrelated&filter=recent", "https://www.threads.com/search?q=need+a+website", "https://www.threads.com/@buyer"} {
		batch.SourceURL = source
		if w := call(batch, "Bearer "+key); w.Code != 400 {
			t.Fatalf("accepted bad source %s", source)
		}
	}
}
func TestIngestRejectsInvalidPosts(t *testing.T) {
	key := strings.Repeat("b", 64)
	tests := []struct {
		name   string
		change func(*browserPost)
	}{
		{"off topic", func(p *browserPost) { p.Text = "Happy birthday to my cat" }},
		{"stale", func(p *browserPost) { p.PostedAt = time.Now().Add(-49 * time.Hour) }},
		{"future", func(p *browserPost) { p.PostedAt = time.Now().Add(time.Hour) }},
		{"missing date", func(p *browserPost) { p.PostedAt = time.Time{} }},
		{"foreign URL", func(p *browserPost) { p.Permalink = "https://evil.example/@buyer/post/Ddv8MUkGt1S" }},
		{"overflow ID", func(p *browserPost) { p.Permalink = "https://www.threads.com/@buyer/post/___________" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &browserTestStore{}
			batch := validBrowserBatch()
			tt.change(&batch.Posts[0])
			data, _ := json.Marshal(batch)
			req := httptest.NewRequest("POST", "/ingest/posts", strings.NewReader(string(data)))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			ingestPostsHandler(key, store, func() { t.Fatal("invalid post triggered worker") }, slog.Default()).ServeHTTP(w, req)
			if w.Code != 200 || len(store.posts) != 0 || !strings.Contains(w.Body.String(), `"rejected":1`) {
				t.Fatalf("invalid post accepted: %s", w.Body.String())
			}
		})
	}
}
func TestIngestFailsClosedAndRedactsStoreError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	key := strings.Repeat("c", 64)
	cases := []struct {
		key, body string
		fail      bool
		code      int
	}{
		{"", `{}`, false, 404},
		{key, `{"unexpected":"field"}`, false, 400},
		{key, `{}` + `{}`, false, 400},
		{key, strings.Repeat("x", 300000), false, 400},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/ingest/posts", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.key)
		w := httptest.NewRecorder()
		ingestPostsHandler(tc.key, &browserTestStore{}, func() {}, logger).ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("want %d got %d", tc.code, w.Code)
		}
	}
	data, _ := json.Marshal(validBrowserBatch())
	req := httptest.NewRequest("POST", "/ingest/posts", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	ingestPostsHandler(key, &browserTestStore{fail: true}, func() {}, logger).ServeHTTP(w, req)
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("storage error leaked or missed")
	}
}
