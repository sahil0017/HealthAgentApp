package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

func votes(spec map[string]int) map[string]*rangeVote {
	out := map[string]*rangeVote{}
	for text, n := range spec {
		v := &rangeVote{Low: fp(1), High: fp(2), Docs: map[string]bool{}}
		for i := 0; i < n; i++ {
			v.Docs[fmt.Sprintf("%s-%d", text, i)] = true
		}
		out[text] = v
	}
	return out
}

func TestChooseRangeNeedsSampleAndAgreement(t *testing.T) {
	if _, agree, total, ok := chooseRange(votes(map[string]int{"70-99": 4})); !ok || agree != 4 || total != 4 {
		t.Error("four agreeing reports should qualify")
	}
	if _, _, _, ok := chooseRange(votes(map[string]int{"70-99": 2})); ok {
		t.Error("two reports are too few")
	}
	if _, _, _, ok := chooseRange(votes(map[string]int{"70-99": 3, "65-95": 3})); ok {
		t.Error("labs that disagree must not produce a single range")
	}
	if best, _, _, ok := chooseRange(votes(map[string]int{"70-99": 8, "65-95": 2})); !ok || len(best.Docs) != 8 {
		t.Error("80% agreement qualifies")
	}
	if _, _, _, ok := chooseRange(nil); ok {
		t.Error("no data, no range")
	}
}

func seedReports(t *testing.T, s *Service, code, unit string, ranges []string, status string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO biomarker_catalog(code,display_name) VALUES($1,$1) ON CONFLICT(code) DO NOTHING`, code); err != nil {
		t.Fatal(err)
	}
	var profile string
	if err := s.DB.Pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name) VALUES('learn-owner','L') RETURNING id::text`).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1::uuid`, profile) })
	for i, r := range ranges {
		var doc string
		if err := s.DB.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1::uuid,'learn-owner',$2,'r.pdf','application/pdf',10,'approved') RETURNING id::text`, profile, fmt.Sprintf("k-%s-%s-%s-%d", code, strings.ReplaceAll(unit, "/", "_"), profile, i)).Scan(&doc); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO observations(profile_id,document_id,owner_id,name,value,unit,biomarker_code,verification_status,reference_range) VALUES($1::uuid,$2::uuid,'learn-owner',$3,5,$4,$3,$5,$6)`, profile, doc, code, unit, status, r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLearningProposesRangesOnlyFromVerifiedAgreeingReports(t *testing.T) {
	s := testService(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	seedReports(t, s, "LRN_A", "mg/dL", []string{"70-99", "70 - 99", "70–99 mg/dL"}, "VERIFIED")
	seedReports(t, s, "LRN_B", "mg/dL", []string{"1-2", "3-4", "5-6"}, "VERIFIED")
	seedReports(t, s, "LRN_C", "mg/dL", []string{"70-99", "70-99", "70-99"}, "PENDING_REVIEW")
	seedReports(t, s, "LRN_D", "mg/dL", []string{"70-99", "70-99"}, "VERIFIED")
	if err := s.LearnFromRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE kind='reference_range' AND status='pending' AND entry_key LIKE 'LRN_A|%'`); n != 1 {
		t.Fatalf("agreeing verified reports should yield one pending proposal, got %d", n)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE kind='reference_range' AND entry_key NOT LIKE 'LRN_A|%'`); n != 0 {
		t.Fatalf("disagreeing, unverified or too-few reports must not propose anything, got %d", n)
	}
	var source string
	if err := s.DB.Pool.QueryRow(ctx, `SELECT payload->>'source' FROM knowledge_entries WHERE kind='reference_range'`).Scan(&source); err != nil || !strings.Contains(source, "3 of 3") {
		t.Fatalf("source should state the sample: %q %v", source, err)
	}
	if err := s.LearnFromRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries WHERE kind='reference_range'`); n != 1 {
		t.Fatalf("learning must be idempotent, got %d", n)
	}
}

func TestLearningQueuesConversionsOncePerUnitPair(t *testing.T) {
	s := testService(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	seedReports(t, s, "LRN_E", "mg/dL", []string{""}, "VERIFIED")
	seedReports(t, s, "LRN_E", "g/L", []string{""}, "VERIFIED")
	seedReports(t, s, "LRN_F", "mg/dL", []string{""}, "VERIFIED")
	for i := 0; i < 2; i++ {
		if err := s.LearnFromRecords(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE kind='conversion'`); n != 1 {
		t.Fatalf("one biomarker with two units needs exactly one conversion task, got %d", n)
	}
	if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO unit_conversions(biomarker_code,from_unit,to_unit,factor,source,approved_by) VALUES('LRN_E','g/L','mg/dL',100,'t','r')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.Pool.Exec(ctx, `DELETE FROM unit_conversions WHERE biomarker_code='LRN_E'`) })
	if _, err := s.DB.Pool.Exec(ctx, `TRUNCATE research_tasks`); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnFromRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE kind='conversion'`); n != 0 {
		t.Fatalf("an approved conversion in either direction ends the need, got %d", n)
	}
}

func TestConversionTaskStoresAPendingCitedProposal(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/research/convert" || r.URL.Query().Get("from_unit") != "mg/dL" || r.URL.Query().Get("to_unit") != "g/L" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"conversion":{"factor":0.01,"offset":0,"evidence":[{"source":"UCUM","url":"https://ucum.nlm.nih.gov/","detail":"x"}]}}`))
	})
	ctx := context.Background()
	if err := s.Enqueue(ctx, "conversion", "LRN_E|mg/dL|g/L"); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("process: %v %v", worked, err)
	}
	var factor float64
	var status, evidence string
	if err := s.DB.Pool.QueryRow(ctx, `SELECT (payload->>'factor')::float8,status,evidence::text FROM knowledge_entries WHERE kind='unit_conversion'`).Scan(&factor, &status, &evidence); err != nil || factor != 0.01 || status != "pending" || !strings.Contains(evidence, "UCUM") {
		t.Fatalf("proposal: %v %v %v %v", factor, status, evidence, err)
	}
}

func TestConversionTaskWithNoConversionProposesNothing(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"conversion":null}`)) })
	ctx := context.Background()
	_ = s.Enqueue(ctx, "conversion", "LRN_E|mg/dL|mmol/L")
	if worked, err := s.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("process: %v %v", worked, err)
	}
	if n := count(t, s, `SELECT count(*) FROM knowledge_entries`); n != 0 {
		t.Fatalf("mass to moles must never be guessed, got %d", n)
	}
	if n := count(t, s, `SELECT count(*) FROM research_tasks WHERE status='done'`); n != 1 {
		t.Fatal("the task should still finish")
	}
}
