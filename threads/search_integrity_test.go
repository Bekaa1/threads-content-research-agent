package threads

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func integrityClient(t *testing.T, respond func(*http.Request) (int, string)) *Client {
	t.Helper()
	return &Client{cfg: Config{Retries: 1}, cache: NewCache(t.TempDir(), false, time.Hour),
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := respond(req)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}}
}

func TestSearchRejectsGenericFeedFallback(t *testing.T) {
	calls := 0
	c := integrityClient(t, func(req *http.Request) (int, string) {
		calls++
		if req.Method == http.MethodGet {
			return 200, `<html>search shell</html>`
		}
		return 200, `{"data":{"feed":{"thread_items":[{"post":{"pk":"frog","caption":{"text":"Found a frog"}}}]}}}`
	})
	posts, err := c.searchPosts(context.Background(), "need a developer")
	if !errors.Is(err, ErrSearchUnavailable) || len(posts) != 0 || calls != 2 {
		t.Fatalf("accepted unverified feed: posts=%v err=%v calls=%d", posts, err, calls)
	}
}

func TestSearchGraphQLUsesSearchConnectionAndCursorOnly(t *testing.T) {
	calls := 0
	c := integrityClient(t, func(req *http.Request) (int, string) {
		calls++
		if req.Method == http.MethodGet {
			return 200, `<html>shell</html>`
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		var vars map[string]any
		if err := json.Unmarshal([]byte(req.Form.Get("variables")), &vars); err != nil {
			t.Fatal(err)
		}
		if vars["query"] != "amoCRM интегратор" {
			t.Fatalf("query lost: %v", vars["query"])
		}
		return 200, `{"data":{"searchResults":{"edges":[{"node":{"thread":{"thread_items":[{"post":{"pk":"buyer","caption":{"text":"Need CRM help"}}}]}}}],"page_info":{"has_next_page":false}},"recommended":{"thread_items":[{"post":{"pk":"frog"}}],"page_info":{"end_cursor":"WRONG","has_next_page":true}}}}`
	})
	var hits []SearchResult
	for hit, err := range c.Search(context.Background(), "amoCRM интегратор", 8) {
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, hit)
	}
	if len(hits) != 1 || hits[0].ID != "buyer" || hits[0].Source != SearchSourceGraphQL || !hits[0].VerifiedSearch() || calls != 2 {
		t.Fatalf("unexpected verified results: %+v calls=%d", hits, calls)
	}
}

func TestSearchDoesNotFallbackAfterAccessFailure(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := integrityClient(t, func(*http.Request) (int, string) { calls++; return status, "unavailable" })
			posts, err := c.searchPosts(context.Background(), "CRM")
			if err == nil || len(posts) != 0 || calls != 1 {
				t.Fatalf("posts=%v err=%v calls=%d", posts, err, calls)
			}
		})
	}
}

func TestSearchDistinguishesEmptyFromUnavailable(t *testing.T) {
	for _, body := range []string{
		`{"data":null}`, `{"data":{}}`,
		`{"errors":[{"message":"denied"}],"data":{"searchResults":{"edges":[]}}}`,
		`{"data":{"searchResults":{"edges":[{"node":{"unexpected":{}}}]}}}`,
	} {
		c := integrityClient(t, func(req *http.Request) (int, string) {
			if req.Method == http.MethodGet {
				return 200, "shell"
			}
			return 200, body
		})
		if posts, err := c.searchPosts(context.Background(), "CRM"); err == nil || len(posts) != 0 {
			t.Fatalf("accepted %s", body)
		}
	}
	calls := 0
	c := integrityClient(t, func(*http.Request) (int, string) {
		calls++
		return 200, `<script type="application/json">{"queryName":"BarcelonaSearchResultsQuery","data":{"searchResults":{"edges":[]}}}</script>`
	})
	if posts, err := c.searchPosts(context.Background(), "CRM"); err != nil || len(posts) != 0 || calls != 1 {
		t.Fatalf("valid empty: %v %v calls=%d", posts, err, calls)
	}
}

func TestSearchRejectsEchoOfDifferentQuery(t *testing.T) {
	c := integrityClient(t, func(*http.Request) (int, string) {
		return 200, `<script type="application/json">{"queryName":"BarcelonaSearchResultsQuery","variables":{"query":"frogs"},"data":{"searchResults":{"edges":[]}}}</script>`
	})
	if _, err := c.searchPosts(context.Background(), "CRM"); !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestSearchLimitPreventsUnnecessaryPagination(t *testing.T) {
	calls := 0
	c := integrityClient(t, func(req *http.Request) (int, string) {
		calls++
		if req.Method != http.MethodGet {
			t.Fatal("unnecessary pagination")
		}
		return 200, strings.Replace(searchFixtureHTML, `]},"recommended"`, `],"page_info":{"end_cursor":"CURSOR","has_next_page":true}},"recommended"`, 1)
	})
	for hit, err := range c.Search(context.Background(), "CRM", 1) {
		if err != nil {
			t.Fatal(err)
		}
		if hit.Source != SearchSourceSSR || hit.SourceURL == "" {
			t.Fatalf("missing provenance: %+v", hit)
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestSearchReportsContinuationRateLimit(t *testing.T) {
	c := integrityClient(t, func(req *http.Request) (int, string) {
		if req.Method == http.MethodPost {
			return 429, "limited"
		}
		return 200, strings.Replace(searchFixtureHTML, `]},"recommended"`, `],"page_info":{"end_cursor":"CURSOR","has_next_page":true}},"recommended"`, 1)
	})
	hits, failures := 0, 0
	for _, err := range c.Search(context.Background(), "CRM", 8) {
		if err == nil {
			hits++
		} else if Code(err) == ExitRateLimit {
			failures++
		} else {
			t.Fatal(err)
		}
	}
	if hits != 1 || failures != 1 {
		t.Fatalf("hits=%d failures=%d", hits, failures)
	}
}
