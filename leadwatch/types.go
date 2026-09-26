package leadwatch

import (
	"context"
	"iter"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type Searcher interface {
	Search(context.Context, string, int) iter.Seq2[threads.SearchResult, error]
}

type Assessment struct {
	PostID    string `json:"post_id"`
	Qualified bool   `json:"qualified"`
	Score     int    `json:"score"`
	Category  string `json:"category"`
	Reason    string `json:"reason"`
	Draft     string `json:"draft"`
}

type Lead struct {
	threads.SearchResult
	Assessment
}

// ScannedPost is the minimal, non-secret view exposed by the read-only
// diagnostic endpoint. It intentionally omits model prompts and reply drafts.
type ScannedPost struct {
	PostID     string     `json:"post_id"`
	Query      string     `json:"query"`
	Text       string     `json:"text"`
	Username   string     `json:"username"`
	Permalink  string     `json:"permalink"`
	PostedAt   *time.Time `json:"posted_at,omitempty"`
	SearchedAt time.Time  `json:"searched_at"`
	Qualified  *bool      `json:"qualified,omitempty"`
	Score      *int       `json:"score,omitempty"`
	Category   string     `json:"category"`
}

type Analyzer interface {
	Analyze(context.Context, []threads.SearchResult, string) ([]Assessment, error)
}

type Notifier interface {
	Send(context.Context, string) error
}

type Store interface {
	InsertNew(context.Context, []threads.SearchResult) ([]threads.SearchResult, error)
	Unclassified(context.Context, int) ([]threads.SearchResult, error)
	SaveAssessment(context.Context, Assessment) error
	PendingNotifications(context.Context, int) ([]Lead, error)
	MarkNotified(context.Context, string) error
}
