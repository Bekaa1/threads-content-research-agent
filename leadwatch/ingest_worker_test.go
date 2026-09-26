package leadwatch

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"testing"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type forbiddenSearcher struct{ t *testing.T }

func (s forbiddenSearcher) Search(context.Context, string, int) iter.Seq2[threads.SearchResult, error] {
	s.t.Fatal("anonymous search in browser mode")
	return nil
}

type countingAnalyzer struct {
	count int
	fail  bool
}

func (a *countingAnalyzer) Analyze(_ context.Context, posts []threads.SearchResult, _ string) ([]Assessment, error) {
	a.count += len(posts)
	if a.fail {
		return nil, errors.New("provider unavailable")
	}
	var result []Assessment
	for _, p := range posts {
		result = append(result, Assessment{PostID: p.ID, Category: "none"})
	}
	return result, nil
}
func TestIngestSharedBudgetAndNoAnonymousRequests(t *testing.T) {
	store := newMemoryStore()
	analyzer := &countingAnalyzer{}
	for i := 0; i < 30; i++ {
		id := fmt.Sprint(i)
		store.posts[id] = threads.SearchResult{ID: id, Source: threads.SearchSourceBrowser, Text: "I build websites"}
	}
	w, _ := NewWorker(forbiddenSearcher{t}, analyzer, &fakeNotifier{}, store, []string{"website"}, "", nil)
	w.UseIngestOnly()
	for i := 0; i < 3; i++ {
		if err := w.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if analyzer.count != 20 {
		t.Fatalf("budget exceeded: %d", analyzer.count)
	}
	w.budgetReset = time.Now().Add(-16 * time.Minute)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if analyzer.count != 30 {
		t.Fatal("queued posts not processed after reset")
	}
}
func TestIngestBudgetCountsFailedAttempts(t *testing.T) {
	store := newMemoryStore()
	for i := 0; i < 10; i++ {
		id := fmt.Sprint(i)
		store.posts[id] = threads.SearchResult{ID: id}
	}
	analyzer := &countingAnalyzer{fail: true}
	w, _ := NewWorker(forbiddenSearcher{t}, analyzer, &fakeNotifier{}, store, []string{"website"}, "", nil)
	w.UseIngestOnly()
	for i := 0; i < 4; i++ {
		_ = w.RunOnce(context.Background())
	}
	if analyzer.count != 20 {
		t.Fatalf("errors bypassed budget: %d", analyzer.count)
	}
}
