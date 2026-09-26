package leadwatch

import (
	"context"
	"iter"

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
