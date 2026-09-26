package threads

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/url"
	"strings"
	"time"
)

var ErrSearchUnavailable = errors.New("Threads search unavailable: no verified keyword-search result connection")

// Search streams keyword search hits from the public server-rendered search
// page. Threads changes the logged-out GraphQL search document frequently, so
// the SSR surface is the primary path and the persisted query remains a
// fallback for older responses or authenticated configurations.
func (c *Client) Search(ctx context.Context, query string, limit int) iter.Seq2[SearchResult, error] {
	return func(yield func(SearchResult, error) bool) {
		posts, err := c.searchPosts(ctx, query, limit)
		n := 0
		for _, p := range posts {
			r := searchResultFromPost(p, query, time.Now())
			if !yield(r, nil) {
				return
			}
			n++
			if limit > 0 && n >= limit {
				return
			}
		}
		if err != nil {
			yield(SearchResult{}, err)
		}
	}
}

func (c *Client) searchPosts(ctx context.Context, query string, limits ...int) ([]Post, error) {
	limit := 0
	if len(limits) > 0 {
		limit = limits[0]
	}
	pageURL := WebBase + "/search?q=" + url.QueryEscape(query)
	html, err := c.getHTML(ctx, pageURL)
	// A rate limit, login wall, or HTTP failure must not trigger a second access
	// path. Only a successful HTML shell may use the strict compatibility route.
	if err != nil {
		return nil, err
	}
	if !searchQueryMatchesSSR(html, query) {
		return nil, ErrSearchUnavailable
	}
	if posts, valid := parseSearchPostsSSR(html); valid {
		if limit > 0 && len(posts) >= limit {
			return posts[:limit], nil
		}
		cursor, hasMore := searchPageInfoSSR(html)
		if hasMore && cursor != "" {
			remaining := 0
			if limit > 0 {
				remaining = limit - len(posts)
			}
			extra, continuationErr := c.graphqlSearchThreads(ctx, query, cursor, remaining)
			return appendUniquePosts(posts, extra), continuationErr
		}
		return posts, nil
	}

	// Keep the existing GraphQL route as a compatibility fallback. It is useful
	// when Threads serves a shell without embedded results and for sessions that
	// still expose the persisted search document.
	return c.graphqlSearch(ctx, query, limit)
}

// Some SSR responses echo the query in their Relay preloader. Reject an
// explicitly different query instead of labelling its results as our request.
func searchQueryMatchesSSR(html, query string) bool {
	var matches func(any) bool
	matches = func(data any) bool {
		switch value := data.(type) {
		case map[string]any:
			if value["queryName"] == "BarcelonaSearchResultsQuery" {
				vars, _ := value["variables"].(map[string]any)
				if echoed, ok := vars["query"].(string); ok &&
					strings.Join(strings.Fields(echoed), " ") != strings.Join(strings.Fields(query), " ") {
					return false
				}
			}
			for _, child := range value {
				if !matches(child) {
					return false
				}
			}
		case []any:
			for _, child := range value {
				if !matches(child) {
					return false
				}
			}
		}
		return true
	}
	for _, raw := range dataSJSBlocks(html) {
		if !strings.Contains(raw, "BarcelonaSearchResultsQuery") {
			continue
		}
		var data any
		if json.Unmarshal([]byte(raw), &data) == nil && !matches(data) {
			return false
		}
	}
	return true
}

func searchPageInfoSSR(html string) (cursor string, hasMore bool) {
	for _, raw := range dataSJSBlocks(html) {
		if !strings.Contains(raw, `"searchResults"`) || !strings.Contains(raw, "page_info") {
			continue
		}
		var data any
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		for _, results := range findSearchResults(data, 0) {
			pageInfo, ok := results["page_info"].(map[string]any)
			if !ok {
				continue
			}
			cursor, _ = pageInfo["end_cursor"].(string)
			hasMore, _ = pageInfo["has_next_page"].(bool)
			return cursor, hasMore
		}
	}
	return "", false
}

func appendUniquePosts(existing, extra []Post) []Post {
	seen := make(map[string]bool, len(existing)+len(extra))
	for _, post := range existing {
		if post.ID != "" {
			seen[post.ID] = true
		}
	}
	for _, post := range extra {
		if post.ID == "" || seen[post.ID] {
			continue
		}
		seen[post.ID] = true
		existing = append(existing, post)
	}
	return existing
}

func searchResultFromPost(p Post, query string, searchedAt time.Time) SearchResult {
	r := SearchResult{
		ID:          p.ID,
		Query:       query,
		Source:      p.SearchSource,
		SourceURL:   WebBase + "/search?q=" + url.QueryEscape(query),
		Shortcode:   p.Shortcode,
		Text:        p.Text,
		Username:    p.Username,
		UserID:      p.UserID,
		Permalink:   p.Permalink,
		Timestamp:   p.Timestamp,
		MediaType:   p.MediaType,
		IsReply:     p.IsReply,
		IsQuotePost: p.IsQuotePost,
		FetchedAt:   p.FetchedAt,
		SearchedAt:  searchedAt,
	}
	if p.LikeCountAvailable {
		v := p.LikeCount
		r.LikeCount = &v
	}
	if p.ReplyCountAvailable {
		v := p.ReplyCount
		r.ReplyCount = &v
	}
	if p.RepostCountAvailable {
		v := p.RepostCount
		r.RepostCount = &v
	}
	if p.QuoteCountAvailable {
		v := p.QuoteCount
		r.QuoteCount = &v
	}
	if p.ViewCountAvailable {
		v := p.ViewCount
		r.ViewCount = &v
	}
	return r
}
