package leadwatch

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(ctx context.Context, db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("database connection is required")
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.New("could not connect to configured database")
	}
	const schema = `CREATE TABLE IF NOT EXISTS leadwatch_posts (
		post_id TEXT PRIMARY KEY,
		query TEXT NOT NULL DEFAULT '',
		shortcode TEXT NOT NULL DEFAULT '',
		post_text TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		permalink TEXT NOT NULL DEFAULT '',
		posted_at TIMESTAMPTZ,
		searched_at TIMESTAMPTZ NOT NULL,
		qualified BOOLEAN,
		score SMALLINT,
		category TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		draft TEXT NOT NULL DEFAULT '',
		classified_at TIMESTAMPTZ,
		notified_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, errors.New("could not initialize leadwatch database table")
	}
	// Existing rows retain an empty source: do not retroactively claim their
	// search provenance was verified. No historical rows are deleted.
	if _, err := db.ExecContext(ctx, `ALTER TABLE leadwatch_posts
		ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS source_url TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, errors.New("could not initialize search provenance columns")
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) InsertNew(ctx context.Context, posts []threads.SearchResult) ([]threads.SearchResult, error) {
	newPosts := make([]threads.SearchResult, 0, len(posts))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	const query = `INSERT INTO leadwatch_posts (post_id,query,shortcode,post_text,username,permalink,posted_at,searched_at,source,source_url)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7::timestamptz,'0001-01-01 00:00:00+00'),$8,$9,$10)
		ON CONFLICT (post_id) DO UPDATE SET
		query=EXCLUDED.query,post_text=EXCLUDED.post_text,source=EXCLUDED.source,source_url=EXCLUDED.source_url,
		posted_at=EXCLUDED.posted_at,shortcode=EXCLUDED.shortcode,username=EXCLUDED.username,permalink=EXCLUDED.permalink,
		searched_at=EXCLUDED.searched_at,classified_at=NULL,qualified=NULL,score=NULL,category='',reason='',draft=''
		WHERE leadwatch_posts.source=''`
	for _, post := range posts {
		if post.ID == "" || post.Text == "" || !post.VerifiedSearch() {
			continue
		}
		searchedAt := post.SearchedAt
		if searchedAt.IsZero() {
			searchedAt = time.Now().UTC()
		}
		postedAt := post.Timestamp
		result, err := tx.ExecContext(ctx, query, post.ID, post.Query, post.Shortcode, post.Text, post.Username, post.Permalink, postedAt, searchedAt, post.Source, post.SourceURL)
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if rows > 0 {
			newPosts = append(newPosts, post)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return newPosts, nil
}

func (s *PostgresStore) Unclassified(ctx context.Context, limit int) ([]threads.SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT post_id,query,shortcode,post_text,username,permalink,posted_at,searched_at,source,source_url
		FROM leadwatch_posts WHERE classified_at IS NULL AND source IN ('threads_search_ssr','threads_search_graphql','threads_browser_search')
		ORDER BY searched_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posts := make([]threads.SearchResult, 0)
	for rows.Next() {
		var p threads.SearchResult
		var postedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Query, &p.Shortcode, &p.Text, &p.Username, &p.Permalink, &postedAt, &p.SearchedAt, &p.Source, &p.SourceURL); err != nil {
			return nil, err
		}
		if postedAt.Valid {
			p.Timestamp = postedAt.Time
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func (s *PostgresStore) RecentPosts(ctx context.Context, limit int) ([]ScannedPost, error) {
	if limit < 1 || limit > 5 {
		return nil, errors.New("recent post limit must be between 1 and 5")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT post_id,query,post_text,username,permalink,posted_at,searched_at,qualified,score,category,source,source_url
		FROM leadwatch_posts ORDER BY searched_at DESC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posts := make([]ScannedPost, 0, limit)
	for rows.Next() {
		var post ScannedPost
		var postedAt sql.NullTime
		var qualified sql.NullBool
		var score sql.NullInt64
		if err := rows.Scan(&post.PostID, &post.Query, &post.Text, &post.Username, &post.Permalink, &postedAt, &post.SearchedAt, &qualified, &score, &post.Category, &post.Source, &post.SourceURL); err != nil {
			return nil, err
		}
		if postedAt.Valid {
			value := postedAt.Time
			post.PostedAt = &value
		}
		if qualified.Valid {
			value := qualified.Bool
			post.Qualified = &value
		}
		if score.Valid {
			value := int(score.Int64)
			post.Score = &value
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

func (s *PostgresStore) SaveAssessment(ctx context.Context, a Assessment) error {
	result, err := s.db.ExecContext(ctx, `UPDATE leadwatch_posts SET qualified=$2,score=$3,category=$4,reason=$5,draft=$6,classified_at=NOW() WHERE post_id=$1 AND classified_at IS NULL`, a.PostID, a.Qualified, a.Score, a.Category, a.Reason, a.Draft)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return errors.New("post assessment was not saved")
	}
	return nil
}

func (s *PostgresStore) PendingNotifications(ctx context.Context, limit int) ([]Lead, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT post_id,query,shortcode,post_text,username,permalink,posted_at,searched_at,qualified,score,category,reason,draft,source,source_url
		FROM leadwatch_posts WHERE classified_at IS NOT NULL AND qualified=TRUE AND notified_at IS NULL
		AND source IN ('threads_search_ssr','threads_search_graphql','threads_browser_search')
		ORDER BY score DESC, searched_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leads := make([]Lead, 0)
	for rows.Next() {
		var lead Lead
		var postedAt sql.NullTime
		if err := rows.Scan(&lead.ID, &lead.Query, &lead.Shortcode, &lead.Text, &lead.Username, &lead.Permalink, &postedAt, &lead.SearchedAt, &lead.Qualified, &lead.Score, &lead.Category, &lead.Reason, &lead.Draft, &lead.Source, &lead.SourceURL); err != nil {
			return nil, err
		}
		if postedAt.Valid {
			lead.Timestamp = postedAt.Time
		}
		lead.Assessment.PostID = lead.ID
		leads = append(leads, lead)
	}
	return leads, rows.Err()
}

func (s *PostgresStore) MarkNotified(ctx context.Context, postID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE leadwatch_posts SET notified_at=NOW() WHERE post_id=$1`, postID)
	return err
}
