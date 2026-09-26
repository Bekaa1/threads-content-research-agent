package leadwatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Egor01KKK/threads-content-research-agent/threads"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Run only against the disposable, isolated test container documented in
// docs/LEADWATCH.md. Never pass the production DATABASE_URL to this test.
func TestPostgresSearchProvenance(t *testing.T) {
	dsn := os.Getenv("LEADWATCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated LEADWATCH_TEST_DATABASE_URL")
	}
	if dsn != "postgres://postgres@127.0.0.1:5432/leadwatch_test?sslmode=disable" {
		t.Fatal("refusing non-test database target")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// The old production schema has no source columns.
	_, err = db.ExecContext(ctx, `CREATE TABLE leadwatch_posts (
		post_id TEXT PRIMARY KEY,query TEXT NOT NULL DEFAULT '',shortcode TEXT NOT NULL DEFAULT '',
		post_text TEXT NOT NULL,username TEXT NOT NULL DEFAULT '',permalink TEXT NOT NULL DEFAULT '',
		posted_at TIMESTAMPTZ,searched_at TIMESTAMPTZ NOT NULL,qualified BOOLEAN,score SMALLINT,
		category TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',draft TEXT NOT NULL DEFAULT '',
		classified_at TIMESTAMPTZ,notified_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO leadwatch_posts(post_id,post_text,searched_at,classified_at,qualified)
		VALUES ('legacy','Old unverified post',NOW(),NOW(),TRUE),('legacy-pending','Old pending post',NOW(),NULL,NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgresStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPostgresStore(ctx, db); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	if posts, err := store.Unclassified(ctx, 10); err != nil || len(posts) != 0 {
		t.Fatalf("legacy entered classifier: %v %v", posts, err)
	}
	if leads, err := store.PendingNotifications(ctx, 10); err != nil || len(leads) != 0 {
		t.Fatalf("legacy entered notifications: %v %v", leads, err)
	}
	post := threads.SearchResult{ID: "buyer", Query: "CRM", Text: "Need a CRM integrator", Username: "buyer", Permalink: "https://www.threads.com/@buyer/post/TEST", Source: threads.SearchSourceSSR, SourceURL: "https://www.threads.com/search?q=CRM", SearchedAt: time.Now()}
	inserted, err := store.InsertNew(ctx, []threads.SearchResult{post, {ID: "noise", Text: "frog"}})
	if err != nil || len(inserted) != 1 {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	if inserted, err := store.InsertNew(ctx, []threads.SearchResult{post}); err != nil || len(inserted) != 0 {
		t.Fatalf("duplicate: %v %v", inserted, err)
	}
	posts, err := store.Unclassified(ctx, 10)
	if err != nil || len(posts) != 1 || posts[0].Source != post.Source || posts[0].SourceURL != post.SourceURL {
		t.Fatalf("provenance lost: %+v %v", posts, err)
	}
	if err := store.SaveAssessment(ctx, Assessment{PostID: "buyer", Qualified: true, Score: 90, Category: "crm"}); err != nil {
		t.Fatal(err)
	}
	leads, err := store.PendingNotifications(ctx, 10)
	if err != nil || len(leads) != 1 || leads[0].Source != post.Source {
		t.Fatalf("leads=%+v err=%v", leads, err)
	}
	if err := store.MarkNotified(ctx, "buyer"); err != nil {
		t.Fatal(err)
	}
	recent, err := store.RecentPosts(ctx, 5)
	if err != nil || len(recent) != 3 {
		t.Fatalf("history not preserved: %+v %v", recent, err)
	}
	// Re-observing an old record in verified search permits reclassification,
	// but never erases the fact that it has already been sent to Telegram.
	if err := store.MarkNotified(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
	post.ID = "legacy"
	if inserted, err := store.InsertNew(ctx, []threads.SearchResult{post}); err != nil || len(inserted) != 1 {
		t.Fatalf("legacy upgrade: %v %v", inserted, err)
	}
	posts, err = store.Unclassified(ctx, 10)
	if err != nil || len(posts) != 1 || posts[0].ID != "legacy" {
		t.Fatalf("legacy not reclassified: %+v %v", posts, err)
	}
	if err := store.SaveAssessment(ctx, Assessment{PostID: "legacy", Qualified: true, Score: 90, Category: "crm"}); err != nil {
		t.Fatal(err)
	}
	if leads, err := store.PendingNotifications(ctx, 10); err != nil || len(leads) != 0 {
		t.Fatalf("duplicate notification after migration: %+v %v", leads, err)
	}
}
