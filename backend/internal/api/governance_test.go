package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cortex/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type fakeRemover struct {
	removed []string
	failOn  string
}

func (f *fakeRemover) Remove(_ context.Context, key string) error {
	if key == f.failOn {
		return errors.New("storage down")
	}
	f.removed = append(f.removed, key)
	return nil
}

func governanceAPI(t *testing.T, ownerID string, remover objectRemover) (*API, http.Handler) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	a := &API{db: db, remover: remover}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), ownerKey{}, ownerID)))
		})
	})
	r.Delete("/profiles/{profileID}", a.deleteProfile)
	r.Get("/profiles/{profileID}/access-history", a.accessHistory)
	r.Get("/profiles/{profileID}/export", a.exportProfile)
	r.Get("/profiles/{profileID}/documents/search", a.searchDocuments)
	r.Patch("/documents/{documentID}", a.updateDocument)
	r.Get("/documents/{documentID}/history", a.documentHistory)
	return a, r
}

// seedProfile creates a profile with a document, an observation, an embedding and audit rows.
func seedProfile(t *testing.T, a *API, ownerID string, objectKeys ...string) string {
	t.Helper()
	ctx := context.Background()
	var profileID string
	if err := a.db.Pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name) VALUES($1,'Test') RETURNING id::text`, ownerID).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = a.db.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, profileID) })
	if _, err := a.db.Pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'ai_processing',true)`, profileID); err != nil {
		t.Fatal(err)
	}
	for i, key := range objectKeys {
		var docID string
		if err := a.db.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1,$2,$3,'r.pdf','application/pdf',10,'approved') RETURNING id::text`, profileID, ownerID, key).Scan(&docID); err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Pool.Exec(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id) VALUES('document.uploaded','medical_document',$1)`, docID); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			var obsID string
			if err := a.db.Pool.QueryRow(ctx, `INSERT INTO observations(profile_id,document_id,owner_id,name,value,unit) VALUES($1,$2,$3,'Glucose',95,'mg/dL') RETURNING id::text`, profileID, docID, ownerID).Scan(&obsID); err != nil {
				t.Fatal(err)
			}
			vector := "[" + strings.TrimSuffix(strings.Repeat("0.1,", 384), ",") + "]"
			if _, err := a.db.Pool.Exec(ctx, `INSERT INTO record_chunks(owner_id,profile_id,observation_id,observation_revision,content,embedding,embedding_model) VALUES($1,$2,$3,1,'Glucose 95',$4::vector,'test')`, ownerID, profileID, obsID, vector); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := a.db.Pool.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type) VALUES($1,$2,'document.upload_completed','medical_document'),($1,$2,'mcp.tool_called','profile')`, ownerID, profileID); err != nil {
		t.Fatal(err)
	}
	return profileID
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	h.ServeHTTP(w, req)
	return w
}

func count(t *testing.T, a *API, query string, args ...any) int {
	t.Helper()
	var n int
	if err := a.db.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAccessHistoryIsOwnerScopedNewestFirstAndPaged(t *testing.T) {
	a, h := governanceAPI(t, "gov-owner", &fakeRemover{})
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")

	w := call(h, "GET", "/profiles/"+id+"/access-history?limit=1", "")
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		Data []accessEvent `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Data) != 1 || body.Data[0].Action != "mcp.tool_called" {
		t.Fatalf("expected newest event first, got %+v", body.Data)
	}
	if w := call(h, "GET", "/profiles/"+id+"/access-history?limit=0", ""); w.Code != 400 {
		t.Fatalf("limit 0 should be rejected, got %d", w.Code)
	}

	_, other := governanceAPI(t, "someone-else", &fakeRemover{})
	if w := call(other, "GET", "/profiles/"+id+"/access-history", ""); w.Code != 404 {
		t.Fatalf("another owner must not read the history, got %d", w.Code)
	}
}

func TestExportContainsRecordsWithoutObjectKeysAndIsAudited(t *testing.T) {
	a, h := governanceAPI(t, "gov-owner", &fakeRemover{})
	id := seedProfile(t, a, "gov-owner", "gov/secret-key.pdf")

	w := call(h, "GET", "/profiles/"+id+"/export", "")
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "gov/secret-key.pdf") {
		t.Fatal("internal object keys must not be exported")
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("export should download as a file")
	}
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	for _, key := range []string{"profile", "consents", "documents", "observations", "observation_revisions", "audit_events", "exported_at"} {
		if _, ok := body.Data[key]; !ok {
			t.Fatalf("export is missing %q", key)
		}
	}
	var observations []map[string]any
	_ = json.Unmarshal(body.Data["observations"], &observations)
	if len(observations) != 1 || observations[0]["name"] != "Glucose" {
		t.Fatalf("unexpected observations: %v", observations)
	}
	if count(t, a, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='profile.exported'`, id) != 1 {
		t.Fatal("export must be audited")
	}

	_, other := governanceAPI(t, "someone-else", &fakeRemover{})
	if w := call(other, "GET", "/profiles/"+id+"/export", ""); w.Code != 404 {
		t.Fatalf("another owner must not export, got %d", w.Code)
	}
}

func TestDeleteRequiresExplicitConfirmation(t *testing.T) {
	a, h := governanceAPI(t, "gov-owner", &fakeRemover{})
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")
	for _, body := range []string{"", `{}`, `{"confirm":"yes"}`} {
		if w := call(h, "DELETE", "/profiles/"+id, body); w.Code != 400 {
			t.Fatalf("body %q: expected 400, got %d", body, w.Code)
		}
	}
	if count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 {
		t.Fatal("profile must survive an unconfirmed delete")
	}
}

func TestDeleteRemovesFilesAndEveryDerivedRowButKeepsAuditMetadata(t *testing.T) {
	remover := &fakeRemover{}
	a, h := governanceAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf", "gov/b.pdf")
	var docIDs []string
	if err := a.db.Pool.QueryRow(context.Background(), `SELECT array_agg(id::text) FROM medical_documents WHERE profile_id=$1`, id).Scan(&docIDs); err != nil {
		t.Fatal(err)
	}

	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 204 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(remover.removed) != 2 {
		t.Fatalf("both files should be removed, got %v", remover.removed)
	}
	for _, table := range []string{"profiles", "consents", "medical_documents", "observations", "record_chunks"} {
		col := "profile_id"
		if table == "profiles" {
			col = "id"
		}
		if n := count(t, a, `SELECT count(*) FROM `+table+` WHERE `+col+`=$1`, id); n != 0 {
			t.Fatalf("%s still has %d rows", table, n)
		}
	}
	if n := count(t, a, `SELECT count(*) FROM outbox_events WHERE aggregate_id=ANY($1::uuid[])`, docIDs); n != 0 {
		t.Fatalf("orphaned outbox events remain: %d", n)
	}
	if count(t, a, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='profile.deleted'`, id) != 1 {
		t.Fatal("deletion must leave an audit event")
	}
	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 404 {
		t.Fatalf("second delete should be 404, got %d", w.Code)
	}
}

func TestDeleteKeepsDataWhenStorageFailsAndCanBeRetried(t *testing.T) {
	remover := &fakeRemover{failOn: "gov/b.pdf"}
	a, h := governanceAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf", "gov/b.pdf")

	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 502 {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	if count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 || count(t, a, `SELECT count(*) FROM observations WHERE profile_id=$1`, id) != 1 {
		t.Fatal("no database rows may be removed when file removal fails")
	}
	remover.failOn = ""
	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 204 {
		t.Fatalf("retry should succeed, got %d", w.Code)
	}
}

func TestDeleteIsOwnerScoped(t *testing.T) {
	remover := &fakeRemover{}
	a, _ := governanceAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")
	_, other := governanceAPI(t, "someone-else", remover)
	if w := call(other, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(remover.removed) != 0 || count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 {
		t.Fatal("a non-owner must not delete anything")
	}
}

func TestDeleteWithFilesButNoStorageFailsSafely(t *testing.T) {
	a, h := governanceAPI(t, "gov-owner", nil)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")
	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 {
		t.Fatal("profile must remain")
	}
}
