package leadwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type Worker struct {
	searcher        Searcher
	analyzer        Analyzer
	notifier        Notifier
	store           Store
	queries         []string
	offer           string
	logger          *slog.Logger
	ingestOnly      bool
	wake            chan struct{}
	runMu           sync.Mutex
	budgetReset     time.Time
	budgetRemaining int
}

const maxLeadAge = 30 * 24 * time.Hour

var errSearchRateLimited = errors.New("Threads search rate limited; remaining queries skipped")
var errOffTopicSearch = errors.New("Threads returned posts unrelated to the requested service topic")

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
	return &Worker{searcher: searcher, analyzer: analyzer, notifier: notifier, store: store, queries: append([]string(nil), queries...), offer: offer, logger: logger, wake: make(chan struct{}, 1)}, nil
}

// Call before Run starts. Browser mode never makes anonymous Threads requests.
func (w *Worker) UseIngestOnly() { w.ingestOnly = true }
func (w *Worker) Trigger() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("worker interval must be positive")
	}
	delay := interval
	for {
		err := w.RunOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			w.logger.Error("lead scan cycle failed", "error", err)
		}
		delay = nextScanDelay(interval, delay, err)
		w.logger.Info("next lead scan scheduled", "delay_seconds", int(delay.Round(time.Second).Seconds()))
		timer := time.NewTimer(delay)
		if errors.Is(err, errGroqRateLimited) {
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			select {
			case <-w.wake:
			default:
			}
			continue
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		case <-w.wake:
			timer.Stop()
		}
	}
}

func nextScanDelay(interval, previous time.Duration, err error) time.Duration {
	var groqLimit *groqRateLimitError
	if errors.As(err, &groqLimit) {
		if groqLimit.retryAfter > 0 {
			return max(groqLimit.retryAfter, time.Second)
		}
		return interval
	}
	if errors.Is(err, errSearchRateLimited) {
		return min(previous*2, max(time.Hour, interval))
	}
	return interval
}

func (w *Worker) RunOnce(ctx context.Context) error {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	if time.Since(w.budgetReset) >= 15*time.Minute {
		w.budgetReset = time.Now()
		w.budgetRemaining = maxNewPerCycle
	}
	var collected []threads.SearchResult
	queries := w.queries
	if w.ingestOnly {
		queries = nil
	}
	succeeded, failed, skipped := 0, 0, 0
	var scanErr error
	for _, query := range queries {
		count := 0
		offTopic := 0
		var queryErr error
		for result, err := range w.searcher.Search(ctx, query, maxSearchPosts) {
			if err != nil {
				queryErr = err
				w.logger.Warn("Threads search query failed", "query", query, "error", err)
				break
			}
			if !result.VerifiedSearch() {
				queryErr = threads.ErrSearchUnavailable
				break
			}
			if strings.TrimSpace(result.ID) == "" || strings.TrimSpace(result.Text) == "" {
				continue
			}
			if !queryTopicMatches(query, result.Text) {
				offTopic++
				continue
			}
			collected = append(collected, result)
			count++
			if count >= maxSearchPosts {
				break
			}
		}
		status := "ok"
		if queryErr == nil && count == 0 && offTopic > 0 {
			queryErr = errOffTopicSearch
		}
		if queryErr != nil {
			failed++
			status = "failed"
			if errors.Is(queryErr, errOffTopicSearch) {
				status = "off_topic"
			}
		} else {
			succeeded++
			if count == 0 {
				status = "empty"
			}
		}
		w.logger.Info("Threads query result", "query", query, "status", status, "verified_posts", count, "off_topic_rejected", offTopic)
		if threads.Code(queryErr) == threads.ExitRateLimit {
			scanErr = errSearchRateLimited
			skipped = len(queries) - succeeded - failed
			break
		}
	}
	newPosts, err := w.store.InsertNew(ctx, collected)
	if err != nil {
		return fmt.Errorf("store newly found posts: %w", err)
	}

	remaining := w.budgetRemaining
	classified := 0
	for remaining > 0 {
		batch, err := w.store.Unclassified(ctx, min(maxBatchSize, remaining))
		if err != nil {
			return fmt.Errorf("load unclassified posts: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		// Count attempts too: repeated ingest requests or provider errors must not
		// spend more than the existing 20-post budget per 15 minutes.
		w.budgetRemaining -= len(batch)
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
		classified += len(batch)
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
	status := "ok"
	if failed > 0 {
		status = "degraded"
		if succeeded == 0 {
			status = "unavailable"
		}
		if scanErr == nil {
			scanErr = fmt.Errorf("Threads search failed for %d of %d queries", failed, len(queries))
		}
	} else if len(collected) == 0 && classified == 0 && len(pending) == 0 {
		status = "empty"
	}
	w.logger.Info("lead scan cycle finished", "status", status, "ingest_only", w.ingestOnly, "queries", len(queries), "queries_succeeded", succeeded, "queries_failed", failed, "queries_skipped", skipped, "posts_seen", len(collected), "new_posts", len(newPosts), "classified", classified, "notifications", len(pending), "classification_budget_remaining", w.budgetRemaining)
	return scanErr
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
