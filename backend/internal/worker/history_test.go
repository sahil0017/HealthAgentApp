package worker

import (
	"context"
	"os"
	"testing"

	"cortex/backend/internal/store"
)

func TestSkippedScanIsQueuedAgainOnceAIIsOn(t *testing.T) {
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
	if _, err := db.Pool.Exec(ctx, `INSERT INTO history_scans(document_id,profile_id,owner_id,status) VALUES($1,$2,$3,'skipped')`, docID, profileID, owner); err != nil {
		t.Fatal(err)
	}
	w := NewHistoryWorker(db)
	status := func() string {
		var s string
		_ = db.Pool.QueryRow(ctx, `SELECT status FROM history_scans WHERE document_id=$1`, docID).Scan(&s)
		return s
	}
	w.catchUp(ctx)
	if status() != "skipped" {
		t.Fatalf("with AI off the scan must stay skipped, got %s", status())
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'ai_processing',true)`, profileID); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"cloud_model", "read_labs"} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO agent_permissions(profile_id,owner_id,permission,granted) VALUES($1,$3,$2,true) ON CONFLICT (profile_id,permission) DO UPDATE SET granted=true`, profileID, p, owner); err != nil {
			t.Fatal(err)
		}
	}
	w.catchUp(ctx)
	if status() != "pending" {
		t.Fatalf("once AI is on the scan must be queued again, got %s", status())
	}
}
