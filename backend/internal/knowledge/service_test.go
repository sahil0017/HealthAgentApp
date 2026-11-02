package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"cortex/backend/internal/store"
)

// testService needs a disposable database and serial packages: TEST_DATABASE_URL=postgres://.../cortex_test go test -p 1 ./...
func testService(t *testing.T, handler http.HandlerFunc) *Service {
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
	if _, err := db.Pool.Exec(ctx, `TRUNCATE research_tasks, knowledge_entries CASCADE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Service{DB: db, Client: &Client{BaseURL: srv.URL, HTTP: srv.Client()}}
}

func count(t *testing.T, s *Service, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func proposal(key string) Proposal {
	return Proposal{Key: key, Payload: json.RawMessage(`{"code":"` + key + `","display_name":"` + key + `"}`), Agreement: 1}
}

func TestEnqueueIsIdempotentAndRequeuesFailures(t *testing.T) {
	s := testService(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.Enqueue(ctx, "biomarker", "Glucose"); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks`); n != 1 {
		t.Fatalf("expected one task, got %d", n)
	}
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE research_tasks SET status='failed', attempts=4`); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(ctx, "biomarker", "Glucose"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE status='queued' AND attempts=0`); n != 1 {
		t.Fatal("a failed task should be requeued with attempts reset")
	}
	if s.Enqueue(ctx, "biomarker", "   ") == nil || s.Enqueue(ctx, "biomarker", strings.Repeat("x", 201)) == nil {
		t.Fatal("blank and oversized terms must be rejected")
	}
}

func TestUpsertNeverOverwritesReviewedEntries(t *testing.T) {
	s := testService(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	if err := s.Upsert(ctx, "biomarker", proposal("GLUCOSE")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE knowledge_entries SET status='rejected'`); err != nil {
		t.Fatal(err)
	}
	changed := proposal("GLUCOSE")
	changed.Agreement = 9
	if err := s.Upsert(ctx, "biomarker", changed); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE status='rejected' AND agreement=1`); n != 1 {
		t.Fatal("a reviewed entry was overwritten")
	}
	if s.Upsert(ctx, "biomarker", Proposal{}) == nil {
		t.Fatal("an empty proposal must be rejected")
	}
}

func TestBootstrapRunsOnlyOnAnEmptyInstall(t *testing.T) {
	s := testService(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	if _, err := s.DB.Pool.Exec(ctx, `DELETE FROM biomarker_catalog WHERE status<>'legacy_seed'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE kind='discover'`); n != 1 {
		t.Fatalf("discovery should be queued once, got %d", n)
	}
}

func TestDiscoverPagesThroughAllBatchesAndStoresPendingProposals(t *testing.T) {
	var offsets []int
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Offset int `json:"offset"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		offsets = append(offsets, in.Offset)
		key := "K" + strconv.Itoa(in.Offset)
		_, _ = w.Write([]byte(`{"proposals":[{"key":"` + key + `","payload":{"code":"` + key + `"},"agreement":1}],"total":100}`))
	})
	ctx := context.Background()
	if err := s.Enqueue(ctx, "discover", "0"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if worked, err := s.ProcessOne(ctx); err != nil || !worked {
			break
		}
	}
	if len(offsets) != 3 || offsets[0] != 0 || offsets[1] != discoverBatch || offsets[2] != 2*discoverBatch {
		t.Fatalf("expected offsets 0, %d, %d; got %v", discoverBatch, 2*discoverBatch, offsets)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE status='pending'`); n != 3 {
		t.Fatalf("expected 3 pending proposals, got %d", n)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE status<>'done'`); n != 0 {
		t.Fatalf("all tasks should be done, %d are not", n)
	}
}

func TestFailedResearchIsRetriedWithBackoffThenGivesUp(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	ctx := context.Background()
	if err := s.Enqueue(ctx, "biomarker", "Glucose"); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE status='queued' AND attempts=1 AND available_at>now() AND last_error<>''`); n != 1 {
		t.Fatal("first failure should requeue with a delay and record the error")
	}
	if worked, _ := s.ProcessOne(ctx); worked {
		t.Fatal("a task still backing off must not be claimed")
	}
	if _, err := s.DB.Pool.Exec(ctx, `UPDATE research_tasks SET attempts=3, available_at=now()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE status='failed'`); n != 1 {
		t.Fatal("task should fail permanently after the final attempt")
	}
}

func TestUnitTaskOnlyProposesValidUcumUnits(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"valid_ucum":` + map[bool]string{true: "true", false: "false"}[r.URL.Query().Get("unit") == "mg/dL"] + `}`))
	})
	ctx := context.Background()
	for _, term := range []string{"GLUCOSE|mg/dL", "GLUCOSE|bogus"} {
		if err := s.Enqueue(ctx, "unit", term); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		_, _ = s.ProcessOne(ctx)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE kind='unit' AND entry_key='GLUCOSE|mg/dL' AND status='pending'`); n != 1 {
		t.Fatal("valid unit should be proposed")
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE entry_key='GLUCOSE|bogus'`); n != 0 {
		t.Fatal("invalid unit must not be proposed")
	}
}
