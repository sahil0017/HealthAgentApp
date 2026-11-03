package api

import (
	"context"
	"cortex/backend/internal/modules/vault"
	"cortex/backend/internal/platform/embedding"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeEmbedder maps iron-related words to one axis and everything else to another, so "anemia" is close to "ferritin".
func fakeEmbedder(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embedding":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "ready", "model": "fake-model"})
		case "/v1/embed":
			var in struct {
				Texts []string `json:"texts"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			vectors := make([][]float32, len(in.Texts))
			for i, text := range in.Texts {
				v := make([]float32, embedding.Dimensions)
				lower := strings.ToLower(text)
				if strings.Contains(lower, "ferritin") || strings.Contains(lower, "iron") || strings.Contains(lower, "anemia") {
					v[0] = 1
				} else {
					v[1] = 1
				}
				vectors[i] = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "fake-model", "vectors": vectors})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AI_SERVICE_URL", srv.URL)
	embedding.ResetModelCache()
}

func searchHits(t *testing.T, h http.Handler, url string) []vault.DocumentSearchHit {
	t.Helper()
	w := call(h, "GET", url, "")
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d body=%s", url, w.Code, w.Body)
	}
	var env struct {
		Data []vault.DocumentSearchHit `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestDocumentSearchFindsMeaningNotJustWords(t *testing.T) {
	fakeEmbedder(t)
	a, h := newAPI(t, "00000000-0000-0000-0000-0000000000a2", nil)
	profileID := seedProfile(t, a, "00000000-0000-0000-0000-0000000000a2", "k/sem-1")
	ctx := context.Background()
	if _, err := a.DB.Pool.Exec(ctx, `INSERT INTO document_extraction_pages(document_id,page_number,extracted_text) SELECT id,1,'Ferritin 45 ng/mL low iron stores' FROM medical_documents WHERE profile_id=$1 LIMIT 1`, profileID); err != nil {
		t.Fatal(err)
	}
	hits := searchHits(t, h, "/profiles/"+profileID+"/documents/search?q=anemia")
	if len(hits) != 1 || hits[0].Match != "semantic" || hits[0].Page != 1 {
		t.Fatalf("semantic search should cite the page: %#v", hits)
	}
	if hits := searchHits(t, h, "/profiles/"+profileID+"/documents/search?q=anemia&mode=keyword"); len(hits) != 0 {
		t.Fatalf("keyword mode must not match by meaning: %#v", hits)
	}
	if n := count(t, a, `SELECT count(*) FROM document_page_chunks WHERE profile_id=$1`, profileID); n != 1 {
		t.Fatalf("chunks = %d, want 1", n)
	}
	// Changed page text is re-embedded rather than served stale.
	if _, err := a.DB.Pool.Exec(ctx, `UPDATE document_extraction_pages SET extracted_text='Grocery list eggs milk' WHERE document_id IN (SELECT id FROM medical_documents WHERE profile_id=$1)`, profileID); err != nil {
		t.Fatal(err)
	}
	if hits := searchHits(t, h, "/profiles/"+profileID+"/documents/search?q=anemia"); len(hits) != 1 || strings.Contains(hits[0].Snippet, "Ferritin") {
		t.Fatalf("stale chunk served after page change: %#v", hits)
	}
	if n := count(t, a, `SELECT count(*) FROM document_page_chunks WHERE profile_id=$1 AND content LIKE 'Grocery%'`, profileID); n != 1 {
		t.Fatal("page was not re-indexed")
	}
	// Derived embeddings must disappear with the document and the profile.
	if _, err := a.DB.Pool.Exec(ctx, `DELETE FROM medical_documents WHERE profile_id=$1`, profileID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, a, `SELECT count(*) FROM document_page_chunks WHERE profile_id=$1`, profileID); n != 0 {
		t.Fatalf("%d chunk(s) outlived their document", n)
	}
}

func TestDocumentSearchWithoutAIConsentUsesKeywordOnly(t *testing.T) {
	fakeEmbedder(t)
	a, h := newAPI(t, "00000000-0000-0000-0000-0000000000a3", nil)
	profileID := seedProfile(t, a, "00000000-0000-0000-0000-0000000000a3", "k/nc-1")
	ctx := context.Background()
	if _, err := a.DB.Pool.Exec(ctx, `UPDATE consents SET granted=false WHERE profile_id=$1`, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Pool.Exec(ctx, `INSERT INTO document_extraction_pages(document_id,page_number,extracted_text) SELECT id,1,'Ferritin 45 ng/mL' FROM medical_documents WHERE profile_id=$1 LIMIT 1`, profileID); err != nil {
		t.Fatal(err)
	}
	hits := searchHits(t, h, "/profiles/"+profileID+"/documents/search?q=ferritin")
	if len(hits) != 1 || hits[0].Match != "keyword" {
		t.Fatalf("want keyword hit, got %#v", hits)
	}
	if n := count(t, a, `SELECT count(*) FROM document_page_chunks WHERE profile_id=$1`, profileID); n != 0 {
		t.Fatal("no text may be embedded without AI consent")
	}
}

func TestDocumentSearchIsScopedCitedAndAudited(t *testing.T) {
	fakeEmbedder(t)
	a, h := newAPI(t, "00000000-0000-0000-0000-0000000000a1", nil)
	profileID := seedProfile(t, a, "00000000-0000-0000-0000-0000000000a1", "k/search-1")
	other := seedProfile(t, a, "00000000-0000-0000-0000-0000000000a1", "k/search-2")
	ctx := context.Background()
	for _, p := range []struct{ profile, text string }{{profileID, "Ferritin 45 ng/mL low iron stores"}, {other, "Ferritin 999 other profile"}} {
		if _, err := a.DB.Pool.Exec(ctx, `INSERT INTO document_extraction_pages(document_id,page_number,extracted_text) SELECT id,1,$2 FROM medical_documents WHERE profile_id=$1 LIMIT 1`, p.profile, p.text); err != nil {
			t.Fatal(err)
		}
	}
	w := call(h, "GET", "/profiles/"+profileID+"/documents/search?q=ferritin", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var env struct {
		Data []vault.DocumentSearchHit `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &env)
	hits := env.Data
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits = %v err=%v body=%s", hits, err, w.Body)
	}
	if hits[0].Page != 1 || hits[0].DocumentID == "" || hits[0].Snippet == "" {
		t.Fatalf("hit must cite document and page: %#v", hits[0])
	}
	if n := count(t, a, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='document.searched'`, profileID); n != 1 {
		t.Fatalf("search audit rows = %d", n)
	}
	if call(h, "GET", "/profiles/"+profileID+"/documents/search?q=x", "").Code != http.StatusBadRequest {
		t.Fatal("short query should be rejected")
	}
	if call(h, "GET", "/profiles/"+profileID+"/documents/search?q=ferritin&document_type=BOGUS", "").Code != http.StatusBadRequest {
		t.Fatal("bad document_type should be rejected")
	}
	if call(h, "GET", "/profiles/00000000-0000-0000-0000-00000000dead/documents/search?q=ferritin", "").Code != http.StatusNotFound {
		t.Fatal("unknown profile should be 404")
	}
}
