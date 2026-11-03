package healthai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/testkit"

	"github.com/google/uuid"
)

// Regression: the hybrid query used to fail with "common column name id appears more than once" whenever
// approved research existed, which silently dropped literature from every answer.
func TestSearchResearchReturnsApprovedLiterature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embedding":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "ready", "model": "fake-model"})
		case "/v1/embed":
			var in struct{ Texts []string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			vectors := make([][]float32, len(in.Texts))
			for i := range vectors {
				v := make([]float32, embedding.Dimensions)
				v[0] = 1
				vectors[i] = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "fake-model", "vectors": vectors})
		}
	}))
	defer srv.Close()
	t.Setenv("AI_SERVICE_URL", srv.URL)
	embedding.ResetModelCache()

	c := testkit.Open(t)
	ctx := context.Background()
	tag := uuid.NewString()[:8]
	var entry, source string
	if err := c.DB.Pool.QueryRow(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload,status) VALUES('research_source',$1,'{}','approved') RETURNING id::text`, "rt-"+tag).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	if err := c.DB.Pool.QueryRow(ctx, `INSERT INTO research_sources(knowledge_entry_id,identifier,title,url) VALUES($1,$2,'Ferritin review','https://pubmed.test/1') RETURNING id::text`, entry, "PMID"+tag).Scan(&source); err != nil {
		t.Fatal(err)
	}
	vec := make([]string, embedding.Dimensions)
	for i := range vec {
		vec[i] = "0"
	}
	vec[0] = "1"
	if _, err := c.DB.Pool.Exec(ctx, `INSERT INTO research_chunks(source_id,ordinal,content,embedding,embedding_model) VALUES($1,0,'Serum ferritin reflects iron stores.',$2::vector,'fake-model')`, source, "["+strings.Join(vec, ",")+"]"); err != nil {
		t.Fatal(err)
	}

	hits, err := New(c).searchResearch(ctx, "ferritin iron stores", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	found := false
	for _, h := range hits {
		found = found || h.Identifier == "PMID"+tag
	}
	if !found {
		t.Fatalf("approved source missing from %+v", hits)
	}
}
