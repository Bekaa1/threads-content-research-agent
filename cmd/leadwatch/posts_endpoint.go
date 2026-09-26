package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Egor01KKK/threads-content-research-agent/leadwatch"
)

const maxDiagnosticPosts = 5

type recentPostReader interface {
	RecentPosts(context.Context, int) ([]leadwatch.ScannedPost, error)
}

func recentPostsHandler(apiKey string, store recentPostReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if apiKey == "" {
			http.NotFound(w, r)
			return
		}
		authorization := r.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authorization, bearerPrefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(authorization, bearerPrefix)), []byte(apiKey)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		limit := maxDiagnosticPosts
		if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
			parsedLimit, err := strconv.Atoi(rawLimit)
			if err != nil || parsedLimit < 1 {
				http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
				return
			}
			if parsedLimit < limit {
				limit = parsedLimit
			}
		}

		posts, err := store.RecentPosts(r.Context(), limit)
		if err != nil {
			http.Error(w, "could not read scanned posts", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Posts []leadwatch.ScannedPost `json:"posts"`
		}{Posts: posts})
	})
}
