package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Egor01KKK/threads-content-research-agent/leadwatch"
)

type fakeRecentPostReader struct {
	limit int
	posts []leadwatch.ScannedPost
	err   error
}

func (f *fakeRecentPostReader) RecentPosts(_ context.Context, limit int) ([]leadwatch.ScannedPost, error) {
	f.limit = limit
	return f.posts, f.err
}

func TestRecentPostsHandlerIsClosedWithoutKey(t *testing.T) {
	reader := &fakeRecentPostReader{}
	recorder := httptest.NewRecorder()
	recentPostsHandler("", reader).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if reader.limit != 0 {
		t.Fatal("database reader was called without an API key")
	}
}

func TestRecentPostsHandlerRejectsUnauthorizedRequest(t *testing.T) {
	reader := &fakeRecentPostReader{}
	recorder := httptest.NewRecorder()
	recentPostsHandler("private-key", reader).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/posts", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if reader.limit != 0 {
		t.Fatal("database reader was called for an unauthorized request")
	}
}

func TestRecentPostsHandlerCapsResultsAtFive(t *testing.T) {
	reader := &fakeRecentPostReader{posts: []leadwatch.ScannedPost{{PostID: "p1", Text: "looking for a developer"}}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/posts?limit=500", nil)
	request.Header.Set("Authorization", "Bearer private-key")
	recentPostsHandler("private-key", reader).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if reader.limit != maxDiagnosticPosts {
		t.Fatalf("reader limit is %d, want %d", reader.limit, maxDiagnosticPosts)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control is %q, want no-store", got)
	}
	if !strings.Contains(recorder.Body.String(), "looking for a developer") {
		t.Fatalf("response omitted post text: %s", recorder.Body.String())
	}
}

func TestRecentPostsHandlerDoesNotLeakDatabaseErrors(t *testing.T) {
	reader := &fakeRecentPostReader{err: errors.New("private database detail")}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	request.Header.Set("Authorization", "Bearer private-key")
	recentPostsHandler("private-key", reader).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "private database detail") {
		t.Fatal("response exposed internal database error")
	}
}
