package research

import (
	"context"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/platform/testkit"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func knowledgeAPI(t *testing.T) (*Module, http.Handler) {
	t.Helper()
	c := testkit.Open(t)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	if _, err := c.DB.Pool.Exec(context.Background(), `TRUNCATE knowledge_entries CASCADE; DELETE FROM biomarker_catalog WHERE source='research-agent'`); err != nil {
		t.Fatal(err)
	}
	m := New(c)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(httpx.WithOwner(req.Context(), "reviewer-1")))
		})
	})
	r.Post("/entries/{entryID}/approve", m.approveKnowledge)
	r.Post("/entries/{entryID}/reject", m.rejectKnowledge)
	r.Post("/bulk", m.bulkApproveKnowledge)
	r.Get("/entries", m.listKnowledge)
	return m, r
}

func addEntry(t *testing.T, a *Module, kind, key, payload string, agreement int) string {
	t.Helper()
	var id string
	err := a.DB.Pool.QueryRow(context.Background(), `INSERT INTO knowledge_entries(kind,entry_key,payload,evidence,agreement) VALUES($1::text,$2::text,$3::text::jsonb,'[]',$4::int) RETURNING id::text`, kind, key, payload, agreement).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, req)
	return w
}

func TestApprovingABiomarkerWritesTheCatalogAndCannotBeRepeated(t *testing.T) {
	a, h := knowledgeAPI(t)
	id := addEntry(t, a, "biomarker", "FERRITIN", `{"code":"ferritin","display_name":"Ferritin","synonyms":["Ferritin"],"supported_units":[],"loinc_nums":["2276-4"]}`, 2)
	if w := post(h, "/entries/"+id+"/approve", `{"note":"ok"}`); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	var status, code string
	if err := a.DB.Pool.QueryRow(context.Background(), `SELECT status, code FROM biomarker_catalog WHERE code='FERRITIN'`).Scan(&status, &code); err != nil || status != "approved" {
		t.Fatalf("catalog row missing or not approved: %v %q", err, status)
	}
	if w := post(h, "/entries/"+id+"/approve", `{}`); w.Code != 409 {
		t.Fatalf("a second review should conflict, got %d", w.Code)
	}
	if w := post(h, "/entries/"+id+"/reject", `{}`); w.Code != 409 {
		t.Fatalf("rejecting a reviewed entry should conflict, got %d", w.Code)
	}
}

func TestRejectedEntriesNeverReachTheCatalog(t *testing.T) {
	a, h := knowledgeAPI(t)
	id := addEntry(t, a, "biomarker", "NOISE", `{"code":"NOISE","display_name":"Noise"}`, 1)
	if w := post(h, "/entries/"+id+"/reject", `{"note":"not a lab test"}`); w.Code != 200 {
		t.Fatalf("reject: %d", w.Code)
	}
	var n int
	_ = a.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM biomarker_catalog WHERE code='NOISE'`).Scan(&n)
	if n != 0 {
		t.Fatal("a rejected proposal entered the catalog")
	}
	if w := post(h, "/entries/not-a-uuid/approve", `{}`); w.Code != 400 {
		t.Fatalf("bad id should be 400, got %d", w.Code)
	}
	if w := post(h, "/entries/00000000-0000-0000-0000-000000000000/approve", `{}`); w.Code != 404 {
		t.Fatalf("unknown id should be 404, got %d", w.Code)
	}
}

func TestUnitApprovalAddsLearnedUnitOnce(t *testing.T) {
	a, h := knowledgeAPI(t)
	post(h, "/entries/"+addEntry(t, a, "biomarker", "GLUCOSE", `{"code":"GLUCOSE","display_name":"Glucose"}`, 1)+"/approve", `{}`)
	u := addEntry(t, a, "unit", "GLUCOSE|mg/dL", `{"code":"GLUCOSE","unit":"mg/dL"}`, 1)
	if w := post(h, "/entries/"+u+"/approve", `{}`); w.Code != 200 {
		t.Fatalf("unit approve: %d %s", w.Code, w.Body)
	}
	var units []string
	if err := a.DB.Pool.QueryRow(context.Background(), `SELECT learned_units FROM biomarker_catalog WHERE code='GLUCOSE'`).Scan(&units); err != nil || len(units) != 1 || units[0] != "mg/dL" {
		t.Fatalf("learned units: %v %v", units, err)
	}
}

func TestBulkApproveSkipsLowAgreementAndNeverTouchesLiterature(t *testing.T) {
	a, h := knowledgeAPI(t)
	addEntry(t, a, "biomarker", "HIGH", `{"code":"HIGH","display_name":"High"}`, 2)
	addEntry(t, a, "biomarker", "LOW", `{"code":"LOW","display_name":"Low"}`, 1)
	lit := addEntry(t, a, "research_source", "PMID:1", `{"identifier":"PMID:1","title":"T","abstract":"A."}`, 1)
	w := post(h, "/bulk", `{"kind":"biomarker","min_agreement":2}`)
	var out struct {
		Data struct{ Approved, Failed int } `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Data.Approved != 1 {
		t.Fatalf("bulk: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/bulk", `{"kind":"research_source"}`); w.Code != 400 {
		t.Fatalf("literature bulk approval must be refused, got %d", w.Code)
	}
	var status string
	_ = a.DB.Pool.QueryRow(context.Background(), `SELECT status FROM knowledge_entries WHERE id=$1::uuid`, lit).Scan(&status)
	if status != "pending" {
		t.Fatalf("literature status changed to %s", status)
	}
}

func TestResearchSourceChunksAreStoredWithTheirEmbeddingModel(t *testing.T) {
	a, _ := knowledgeAPI(t)
	ctx := context.Background()
	id := addEntry(t, a, "research_source", "PMID:42", `{}`, 1)
	vec := make([]float32, 384)
	vec[0] = 1
	tx, err := a.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"identifier":"PMID:42","title":"Title","journal":"J","year":2024,"url":"https://pubmed.ncbi.nlm.nih.gov/42/","abstract":"x"}`)
	if err := applyKnowledge(ctx, tx, "research_source", id, payload, "r", "test-model", []embeddedChunk{{"Title\npassage", vec}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var model, content string
	if err := a.DB.Pool.QueryRow(ctx, `SELECT embedding_model, content FROM research_chunks`).Scan(&model, &content); err != nil || model != "test-model" || content != "Title\npassage" {
		t.Fatalf("chunk: %v %q %q", err, model, content)
	}
}

func TestApplyKnowledgeRejectsMalformedPayloads(t *testing.T) {
	a, _ := knowledgeAPI(t)
	ctx := context.Background()
	for kind, payload := range map[string]string{"biomarker": `{"code":""}`, "unit": `{"code":"X"}`, "other": `{}`} {
		tx, _ := a.DB.Pool.Begin(ctx)
		if applyKnowledge(ctx, tx, kind, "00000000-0000-0000-0000-000000000000", json.RawMessage(payload), "r", "", nil) == nil {
			t.Errorf("%s payload %s was accepted", kind, payload)
		}
		_ = tx.Rollback(ctx)
	}
}
