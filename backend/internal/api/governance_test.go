package api

import (
	"context"
	"cortex/backend/internal/modules/governance"
	"encoding/json"
	"strings"
	"testing"
)

func TestAccessHistoryIsOwnerScopedNewestFirstAndPaged(t *testing.T) {
	a, h := newAPI(t, "gov-owner", &fakeRemover{})
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")

	w := call(h, "GET", "/profiles/"+id+"/access-history?limit=1", "")
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		Data []governance.AccessEvent `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Data) != 1 || body.Data[0].Action != "mcp.tool_called" {
		t.Fatalf("expected newest event first, got %+v", body.Data)
	}
	if w := call(h, "GET", "/profiles/"+id+"/access-history?limit=0", ""); w.Code != 400 {
		t.Fatalf("limit 0 should be rejected, got %d", w.Code)
	}

	_, other := newAPI(t, "someone-else", &fakeRemover{})
	if w := call(other, "GET", "/profiles/"+id+"/access-history", ""); w.Code != 404 {
		t.Fatalf("another owner must not read the history, got %d", w.Code)
	}
}

func TestExportContainsRecordsWithoutObjectKeysAndIsAudited(t *testing.T) {
	a, h := newAPI(t, "gov-owner", &fakeRemover{})
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

	_, other := newAPI(t, "someone-else", &fakeRemover{})
	if w := call(other, "GET", "/profiles/"+id+"/export", ""); w.Code != 404 {
		t.Fatalf("another owner must not export, got %d", w.Code)
	}
}

func TestDeleteRequiresExplicitConfirmation(t *testing.T) {
	a, h := newAPI(t, "gov-owner", &fakeRemover{})
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
	a, h := newAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf", "gov/b.pdf")
	var docIDs []string
	if err := a.DB.Pool.QueryRow(context.Background(), `SELECT array_agg(id::text) FROM medical_documents WHERE profile_id=$1`, id).Scan(&docIDs); err != nil {
		t.Fatal(err)
	}

	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 204 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(remover.Removed) != 2 {
		t.Fatalf("both files should be removed, got %v", remover.Removed)
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
	remover := &fakeRemover{FailOn: "gov/b.pdf"}
	a, h := newAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf", "gov/b.pdf")

	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 502 {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	if count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 || count(t, a, `SELECT count(*) FROM observations WHERE profile_id=$1`, id) != 1 {
		t.Fatal("no database rows may be removed when file removal fails")
	}
	remover.FailOn = ""
	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 204 {
		t.Fatalf("retry should succeed, got %d", w.Code)
	}
}

func TestDeleteIsOwnerScoped(t *testing.T) {
	remover := &fakeRemover{}
	a, _ := newAPI(t, "gov-owner", remover)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")
	_, other := newAPI(t, "someone-else", remover)
	if w := call(other, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(remover.Removed) != 0 || count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 {
		t.Fatal("a non-owner must not delete anything")
	}
}

func TestDeleteWithFilesButNoStorageFailsSafely(t *testing.T) {
	a, h := newAPI(t, "gov-owner", nil)
	id := seedProfile(t, a, "gov-owner", "gov/a.pdf")
	if w := call(h, "DELETE", "/profiles/"+id, `{"confirm":"DELETE"}`); w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if count(t, a, `SELECT count(*) FROM profiles WHERE id=$1`, id) != 1 {
		t.Fatal("profile must remain")
	}
}
