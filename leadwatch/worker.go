package leadwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type Worker struct {
	searcher Searcher
	analyzer Analyzer
	notifier Notifier
	store    Store
	queries  []string
	offer    string
	logger   *slog.Logger
}

const maxLeadAge = 30 * 24 * time.Hour

func NewWorker(searcher Searcher, analyzer Analyzer, notifier Notifier, store Store, queries []string, offer string, logger *slog.Logger) (*Worker, error) {
	if searcher == nil || analyzer == nil || notifier == nil || store == nil {
		return nil, errors.New("worker dependencies are required")
	}
	if len(queries) == 0 {
		return nil, errors.New("at least one search query is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{searcher: searcher, analyzer: analyzer, notifier: notifier, store: store, queries: append([]string(nil), queries...), offer: offer, logger: logger}, nil
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("worker interval must be positive")
	}
	if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
		w.logger.Error("lead scan cycle failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
				w.logger.Error("lead scan cycle failed", "error", err)
			}
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	var collected []threads.SearchResult
	for _, query := range w.queries {
		count := 0
		for result, err := range w.searcher.Search(ctx, query, maxSearchPosts) {
			if err != nil {
				w.logger.Warn("Threads search query failed", "query", query, "error", err)
				break
			}
			if strings.TrimSpace(result.ID) == "" || strings.TrimSpace(result.Text) == "" {
				continue
			}
			collected = append(collected, result)
			count++
			if count >= maxSearchPosts {
				break
			}
		}
	}
	newPosts, err := w.store.InsertNew(ctx, collected)
	if err != nil {
		return fmt.Errorf("store newly found posts: %w", err)
	}

	remaining := maxNewPerCycle
	for remaining > 0 {
		batch, err := w.store.Unclassified(ctx, min(maxBatchSize, remaining))
		if err != nil {
			return fmt.Errorf("load unclassified posts: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		assessments, err := w.analyzer.Analyze(ctx, batch, w.offer)
		if err != nil {
			return fmt.Errorf("classify posts: %w", err)
		}
		if err := validateAssessments(batch, assessments); err != nil {
			return err
		}
		postedAtByID := make(map[string]time.Time, len(batch))
		for _, post := range batch {
			postedAtByID[post.ID] = post.Timestamp
		}
		for _, assessment := range assessments {
			if assessment.Score < minLeadScore || !validCategory(assessment.Category) {
				assessment.Qualified = false
			}
			postedAt := postedAtByID[assessment.PostID]
			if !postedAt.IsZero() && time.Since(postedAt) > maxLeadAge {
				assessment.Qualified = false
				assessment.Category = "none"
				assessment.Draft = ""
				assessment.Reason = "post is older than the 30-day lead window"
			}
			if assessment.Score < 0 {
				assessment.Score = 0
			}
			if assessment.Score > 100 {
				assessment.Score = 100
			}
			if err := w.store.SaveAssessment(ctx, assessment); err != nil {
				return fmt.Errorf("save post assessment: %w", err)
			}
		}
		remaining -= len(batch)
	}

	pending, err := w.store.PendingNotifications(ctx, maxNewPerCycle)
	if err != nil {
		return fmt.Errorf("load leads to notify: %w", err)
	}
	for _, lead := range pending {
		if err := w.notifier.Send(ctx, formatTelegramLead(lead)); err != nil {
			return fmt.Errorf("send Telegram notification: %w", err)
		}
		if err := w.store.MarkNotified(ctx, lead.ID); err != nil {
			return fmt.Errorf("mark lead notified: %w", err)
		}
		if len(pending) > 1 {
			timer := time.NewTimer(3 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	w.logger.Info("lead scan cycle complete", "queries", len(w.queries), "posts_seen", len(collected), "new_posts", len(newPosts), "notifications", len(pending))
	return nil
}

func validateAssessments(posts []threads.SearchResult, assessments []Assessment) error {
	allowed := make(map[string]bool, len(posts))
	for _, post := range posts {
		allowed[post.ID] = true
	}
	if len(assessments) != len(posts) {
		return errors.New("classifier returned an incomplete result set")
	}
	seen := make(map[string]bool, len(assessments))
	for _, assessment := range assessments {
		if !allowed[assessment.PostID] || seen[assessment.PostID] {
			return errors.New("classifier returned an unknown or duplicate post id")
		}
		seen[assessment.PostID] = true
	}
	return nil
}

func validCategory(category string) bool {
	switch category {
	case "website", "crm", "automation", "integration", "bot_or_ai", "mobile_app", "custom_software":
		return true
	default:
		return false
	}
}
