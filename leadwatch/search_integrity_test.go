package leadwatch

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type failingSearcher struct {
	calls int
	err   error
}

func (f *failingSearcher) Search(context.Context, string, int) iter.Seq2[threads.SearchResult, error] {
	return func(yield func(threads.SearchResult, error) bool) { f.calls++; yield(threads.SearchResult{}, f.err) }
}

func TestFailedSearchIsNotReportedAsSuccessfulCycle(t *testing.T) {
	for _, test := range []struct {
		err   error
		calls int
	}{
		{threads.ErrSearchUnavailable, 2},
		{&threads.CodeError{Code: threads.ExitRateLimit, Msg: "limited"}, 1},
	} {
		searcher := &failingSearcher{err: test.err}
		var logs strings.Builder
		worker, _ := NewWorker(searcher, fakeAnalyzer{}, &fakeNotifier{}, newMemoryStore(), []string{"CRM", "website"}, "", slog.New(slog.NewJSONHandler(&logs, nil)))
		err := worker.RunOnce(context.Background())
		if err == nil || searcher.calls != test.calls || !strings.Contains(logs.String(), `"status":"unavailable"`) {
			t.Fatalf("err=%v calls=%d logs=%s", err, searcher.calls, logs.String())
		}
		if test.calls == 1 && !errors.Is(err, errSearchRateLimited) {
			t.Fatalf("rate-limit signal lost: %v", err)
		}
	}
}

func TestWorkerRejectsUnverifiedPostsBeforeStoreOrModel(t *testing.T) {
	store := newMemoryStore()
	worker, _ := NewWorker(fakeSearcher{posts: map[string][]threads.SearchResult{"q": {{ID: "noise", Text: "We need a developer"}}}}, fakeAnalyzer{}, &fakeNotifier{}, store, []string{"q"}, "", slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("unverified result accepted")
	}
	if len(store.posts) != 0 || len(store.assessments) != 0 {
		t.Fatal("unverified post entered pipeline")
	}
}

func TestRateLimitBackoff(t *testing.T) {
	if got := nextScanDelay(15*time.Minute, 15*time.Minute, errSearchRateLimited); got != 30*time.Minute {
		t.Fatal(got)
	}
	if got := nextScanDelay(15*time.Minute, time.Hour, errSearchRateLimited); got != time.Hour {
		t.Fatal(got)
	}
	if got := nextScanDelay(15*time.Minute, time.Hour, nil); got != 15*time.Minute {
		t.Fatal(got)
	}
	if got := nextScanDelay(2*time.Hour, 2*time.Hour, errSearchRateLimited); got != 2*time.Hour {
		t.Fatal(got)
	}
}

func TestTelegramShowsSearchProvenance(t *testing.T) {
	message := formatTelegramLead(Lead{SearchResult: threads.SearchResult{Query: "CRM <help>", Source: threads.SearchSourceSSR, Timestamp: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)}})
	for _, want := range []string{"Запрос: CRM &lt;help&gt;", "Источник: threads_search_ssr", "Дата поста (UTC): 2026-09-26 10:00"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing %s in %s", want, message)
		}
	}
}
