package leadwatch

import (
	"context"
	"iter"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type fakeSearcher struct {
	posts map[string][]threads.SearchResult
}

func (f fakeSearcher) Search(_ context.Context, query string, limit int) iter.Seq2[threads.SearchResult, error] {
	return func(yield func(threads.SearchResult, error) bool) {
		for i, post := range f.posts[query] {
			if i >= limit || !yield(post, nil) {
				return
			}
		}
	}
}

type fakeAnalyzer struct{}

func (fakeAnalyzer) Analyze(_ context.Context, posts []threads.SearchResult, _ string) ([]Assessment, error) {
	result := make([]Assessment, 0, len(posts))
	for _, post := range posts {
		result = append(result, Assessment{PostID: post.ID, Qualified: strings.Contains(post.Text, "need a developer"), Score: 88, Category: "website", Reason: "explicit request", Draft: "What are you looking to build?"})
	}
	return result, nil
}

type fakeNotifier struct{ messages []string }

func (f *fakeNotifier) Send(_ context.Context, message string) error {
	f.messages = append(f.messages, message)
	return nil
}

type memoryStore struct {
	posts       map[string]threads.SearchResult
	assessments map[string]Assessment
	notified    map[string]bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{posts: map[string]threads.SearchResult{}, assessments: map[string]Assessment{}, notified: map[string]bool{}}
}
func (s *memoryStore) InsertNew(_ context.Context, posts []threads.SearchResult) ([]threads.SearchResult, error) {
	var added []threads.SearchResult
	for _, p := range posts {
		if _, exists := s.posts[p.ID]; !exists {
			s.posts[p.ID] = p
			added = append(added, p)
		}
	}
	return added, nil
}
func (s *memoryStore) Unclassified(_ context.Context, limit int) ([]threads.SearchResult, error) {
	var result []threads.SearchResult
	for id, post := range s.posts {
		if _, classified := s.assessments[id]; !classified {
			result = append(result, post)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
func (s *memoryStore) SaveAssessment(_ context.Context, a Assessment) error {
	s.assessments[a.PostID] = a
	return nil
}
func (s *memoryStore) PendingNotifications(_ context.Context, limit int) ([]Lead, error) {
	var result []Lead
	for id, post := range s.posts {
		a := s.assessments[id]
		if a.Qualified && !s.notified[id] {
			result = append(result, Lead{SearchResult: post, Assessment: a})
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
func (s *memoryStore) MarkNotified(_ context.Context, id string) error {
	s.notified[id] = true
	return nil
}

func TestRunOnceNotifiesQualifiedPostOnlyAndDeduplicates(t *testing.T) {
	posts := []threads.SearchResult{
		{ID: "buyer-1", Query: "q", Username: "prospect", Text: "We need a developer for our website", Permalink: "https://www.threads.com/@prospect/post/abc"},
		{ID: "seller-1", Query: "q", Username: "seller", Text: "I am a developer looking for clients"},
		{ID: "old-buyer", Query: "q", Username: "old", Text: "We need a developer", Timestamp: time.Now().Add(-60 * 24 * time.Hour)},
	}
	searcher := fakeSearcher{posts: map[string][]threads.SearchResult{"q": posts}}
	store := newMemoryStore()
	notifier := &fakeNotifier{}
	worker, err := NewWorker(searcher, fakeAnalyzer{}, notifier, store, []string{"q"}, "software services", slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifier.messages))
	}
	if store.assessments["old-buyer"].Qualified {
		t.Fatal("stale post was marked as qualified")
	}
	if !strings.Contains(notifier.messages[0], "Новый лид") || !strings.Contains(notifier.messages[0], "developer") {
		t.Fatalf("unexpected notification: %s", notifier.messages[0])
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("duplicate notification sent; got %d", len(notifier.messages))
	}
}

func TestValidateAssessmentsRejectsMissingAndUnknownIDs(t *testing.T) {
	posts := []threads.SearchResult{{ID: "known"}}
	for _, assessments := range [][]Assessment{{}, {{PostID: "unknown"}}} {
		if err := validateAssessments(posts, assessments); err == nil {
			t.Fatalf("expected invalid assessment set %v", assessments)
		}
	}
}

func TestParseQueriesTrimsAndDeduplicates(t *testing.T) {
	got := parseQueries(" one | two\n one |  ")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("unexpected queries: %#v", got)
	}
}

func TestFormatTelegramLeadEscapesUntrustedText(t *testing.T) {
	message := formatTelegramLead(Lead{SearchResult: threads.SearchResult{Text: "<script>alert(1)</script>", Username: "<user>"}, Assessment: Assessment{Category: "website", Score: 91}})
	if strings.Contains(message, "<script>") || !strings.Contains(message, "&lt;script&gt;") || !strings.Contains(message, "&lt;user&gt;") {
		t.Fatalf("message did not escape untrusted text: %s", message)
	}
}
