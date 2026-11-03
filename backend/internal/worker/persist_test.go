package worker

import (
	"context"
	"os"
	"testing"

	"cortex/backend/internal/processing"
	"cortex/backend/internal/store"
)

func TestPersistSuggestsTypeOnlyWhenOwnerLeftItUnset(t *testing.T) {
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
	const owner = "00000000-0000-0000-0000-0000000000b1"
	var profileID string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name) VALUES($1,'T') RETURNING id::text`, owner).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, profileID) })

	w := &DocumentWorker{DB: db}
	text := "Laboratory Report\nHemoglobin 13.2 g/dL\nReference range 12-16"
	for name, tc := range map[string]struct{ initial, want string }{
		"unset type gets suggestion": {"OTHER", "LAB_REPORT"},
		"owner choice is kept":       {"PRESCRIPTION", "PRESCRIPTION"},
	} {
		var docID, eventID string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status,document_type) VALUES($1,$2,$3,'r.pdf','application/pdf',10,'processing',$4) RETURNING id::text`, profileID, owner, "k/"+name, tc.initial).Scan(&docID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id) VALUES('document.uploaded','medical_document',$1) RETURNING id::text`, docID).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		d := documentInput{ID: docID, ProfileID: profileID, OwnerID: owner}
		if err := w.persist(ctx, eventID, d, processing.Result{PageCount: 1, Pages: []string{text}, Text: text}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got string
		if err := db.Pool.QueryRow(ctx, `SELECT document_type FROM medical_documents WHERE id=$1`, docID).Scan(&got); err != nil || got != tc.want {
			t.Errorf("%s: document_type = %q (err %v), want %q", name, got, err, tc.want)
		}
		var suggested int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='document.type_suggested'`, docID).Scan(&suggested)
		if wantAudit := tc.initial == "OTHER"; (suggested == 1) != wantAudit {
			t.Errorf("%s: type_suggested audit rows = %d", name, suggested)
		}
	}
}
