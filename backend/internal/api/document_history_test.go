package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDocumentHistoryRecordsOwnerEditsWithPreviousValues(t *testing.T) {
	const ownerID = "00000000-0000-0000-0000-0000000000a4"
	a, h := governanceAPI(t, ownerID, nil)
	profileID := seedProfile(t, a, ownerID, "k/hist-1")
	var docID string
	if err := a.db.Pool.QueryRow(context.Background(), `SELECT id::text FROM medical_documents WHERE profile_id=$1`, profileID).Scan(&docID); err != nil {
		t.Fatal(err)
	}
	if w := call(h, "PATCH", "/documents/"+docID, `{"document_type":"lab_report","filename":"cbc.pdf"}`); w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	if w := call(h, "PATCH", "/documents/"+docID, `{"document_type":"PRESCRIPTION"}`); w.Code != http.StatusOK {
		t.Fatalf("patch 2: %d %s", w.Code, w.Body)
	}
	// An access event must not appear in the document history.
	if _, err := a.db.Pool.Exec(context.Background(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'document.file_accessed','medical_document',$3)`, ownerID, profileID, docID); err != nil {
		t.Fatal(err)
	}
	w := call(h, "GET", "/documents/"+docID+"/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	var env struct {
		Data []struct {
			Action  string         `json:"action"`
			Details map[string]any `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 2 {
		t.Fatalf("want the 2 edits, got %d: %s", len(env.Data), w.Body)
	}
	newest := env.Data[0]
	if newest.Action != "document.metadata_updated" || newest.Details["previous_document_type"] != "LAB_REPORT" || newest.Details["document_type"] != "PRESCRIPTION" {
		t.Fatalf("newest edit lost its previous value: %#v", newest)
	}
	for _, e := range env.Data {
		if e.Action == "document.file_accessed" {
			t.Fatal("access events must not be in document history")
		}
	}
	other, oh := governanceAPI(t, "00000000-0000-0000-0000-0000000000a5", nil)
	_ = other
	if call(oh, "GET", "/documents/"+docID+"/history", "").Code != http.StatusNotFound {
		t.Fatal("another owner must get 404")
	}
}
