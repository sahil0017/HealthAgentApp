package worker

import (
	"context"
	"os"
	"testing"

	"cortex/backend/internal/store"
)

func TestPendingHistoryScanIsSkippedWithoutCreatingConditions(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	const owner = "00000000-0000-0000-0000-0000000000b2"
	var profileID, docID string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name) VALUES($1,'T') RETURNING id::text`, owner).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, profileID) })
	if err := db.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1,$2,'k/hs','r.pdf','application/pdf',10,'processing') RETURNING id::text`, profileID, owner).Scan(&docID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO history_scans(document_id,profile_id,owner_id,status) VALUES($1,$2,$3,'pending')`, docID, profileID, owner); err != nil {
		t.Fatal(err)
	}
	w := NewHistoryWorker(db)
	status := func() string {
		var s string
		_ = db.Pool.QueryRow(ctx, `SELECT status FROM history_scans WHERE document_id=$1`, docID).Scan(&s)
		return s
	}
	did, err := w.ProcessOne(ctx)
	if err != nil || !did || status() != "skipped" {
		t.Fatalf("scan: did=%v err=%v status=%s", did, err, status())
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM history_entries WHERE profile_id=$1`, profileID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected history: count=%d err=%v", count, err)
	}
	did, err = w.ProcessOne(ctx)
	if err != nil || did {
		t.Fatalf("skipped scan must not requeue: did=%v err=%v", did, err)
	}
}
