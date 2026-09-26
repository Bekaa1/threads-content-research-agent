package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/leadwatch"
	"github.com/Egor01KKK/threads-content-research-agent/pkg/thid"
	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type ingestStore interface {
	InsertNew(context.Context, []threads.SearchResult) ([]threads.SearchResult, error)
}
type browserPost struct {
	Permalink string    `json:"permalink"`
	Text      string    `json:"text"`
	PostedAt  time.Time `json:"posted_at"`
}
type browserBatch struct {
	Query     string        `json:"query"`
	SourceURL string        `json:"source_url"`
	Posts     []browserPost `json:"posts"`
}

var browserPostPath = regexp.MustCompile(`^/@([A-Za-z0-9._]{1,40})/post/([A-Za-z0-9_-]{6,11})/?$`)

func ingestPostsHandler(key string, store ingestStore, trigger func(), logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if len(key) < 32 {
			http.NotFound(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+key)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var batch browserBatch
		if decoder.Decode(&batch) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid JSON batch", 400)
			return
		}
		batch.Query = strings.TrimSpace(batch.Query)
		u, err := url.Parse(batch.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host != "www.threads.com" || u.User != nil || u.Path != "/search" || u.Query().Get("q") != batch.Query || u.Query().Get("filter") != "recent" || len(batch.Query) < 2 || len([]rune(batch.Query)) > 200 || len(batch.Posts) > 32 {
			http.Error(w, "invalid search source or batch size", 400)
			return
		}
		canonicalSource := "https://www.threads.com/search?" + url.Values{"q": {batch.Query}, "filter": {"recent"}}.Encode()
		posts := make([]threads.SearchResult, 0, len(batch.Posts))
		now := time.Now().UTC()
		for _, post := range batch.Posts {
			link, err := url.Parse(post.Permalink)
			if err != nil || link.Scheme != "https" || link.Host != "www.threads.com" || link.User != nil {
				continue
			}
			parts := browserPostPath.FindStringSubmatch(link.Path)
			if len(parts) != 3 || post.PostedAt.IsZero() || post.PostedAt.After(now.Add(5*time.Minute)) || post.PostedAt.Before(now.Add(-30*24*time.Hour)) || len([]rune(post.Text)) > 10000 || !leadwatch.MatchesQueryTopic(batch.Query, post.Text) {
				continue
			}
			id := thid.ShortcodeToPK(parts[2])
			if id == "" || thid.PKToShortcode(id) != parts[2] {
				continue
			}
			posts = append(posts, threads.SearchResult{ID: id, Query: batch.Query, Shortcode: parts[2], Text: strings.TrimSpace(post.Text), Username: parts[1], Permalink: "https://www.threads.com/@" + parts[1] + "/post/" + parts[2], Timestamp: post.PostedAt, SearchedAt: now, Source: threads.SearchSourceBrowser, SourceURL: canonicalSource})
		}
		stored, err := store.InsertNew(r.Context(), posts)
		if err != nil {
			http.Error(w, "could not store posts", 500)
			return
		}
		if len(stored) > 0 {
			trigger()
		}
		logger.Info("browser batch received", "query", batch.Query, "received", len(batch.Posts), "accepted", len(posts), "stored", len(stored), "rejected", len(batch.Posts)-len(posts))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(posts), "stored": len(stored), "rejected": len(batch.Posts) - len(posts)})
	})
}
