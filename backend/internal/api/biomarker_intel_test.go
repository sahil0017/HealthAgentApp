package api

import (
	"context"
	"fmt"
	"testing"
)

// approve pushes a proposal through the same reviewed path the research agent uses.
func (e *ccEnv) approve(t *testing.T, kind, key, payload string) {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := e.a.db.Pool.QueryRow(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload) VALUES($1::text,$2::text,$3::text::jsonb) ON CONFLICT(kind,entry_key) DO UPDATE SET payload=EXCLUDED.payload,status='pending' RETURNING id::text`, kind, key, payload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := e.a.approveEntry(ctx, id, "reviewer-1", ""); err != nil {
		t.Fatalf("approve %s: %v", kind, err)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM reference_ranges WHERE knowledge_entry_id=$1::uuid`, `DELETE FROM unit_conversions WHERE knowledge_entry_id=$1::uuid`, `DELETE FROM knowledge_entries WHERE id=$1::uuid`} {
			_, _ = e.a.db.Pool.Exec(context.Background(), q, id)
		}
	})
}

func (e *ccEnv) observeFull(t *testing.T, code string, value float64, unit, on, status, sourceType, labRange string) {
	t.Helper()
	if _, err := e.a.db.Pool.Exec(context.Background(), `INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,verification_status,source_type,reference_range) VALUES($1,$2,$3,$3,$4,$5,$6::text::date,$7,$8,NULLIF($9::text,''))`, e.profile, e.owner, code, value, unit, on, status, sourceType, labRange); err != nil {
		t.Fatal(err)
	}
}

func (e *ccEnv) trend(t *testing.T, code, query string) biomarkerTrend {
	t.Helper()
	status, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/"+code+"/trend"+query, "")
	if status != 200 {
		t.Fatalf("trend status %d", status)
	}
	return data[biomarkerTrend](t, env)
}

func TestTrendConvertsOnlyThroughApprovedConversions(t *testing.T) {
	e := commandCentre(t, "bm-convert", storage()...)
	e.observe(t, "GLUCOSE", "Glucose", 100, "mg/dL", ago(60), "VERIFIED")
	e.observe(t, "GLUCOSE", "Glucose", 5.4, "mmol/L", ago(5), "VERIFIED")

	before := e.trend(t, "GLUCOSE", "")
	if before.CanonicalUnit != "mmol/L" || len(before.Points) != 1 || len(before.Excluded) != 1 || before.Excluded[0].Reason != "no_approved_conversion" {
		t.Fatalf("without a conversion the mg/dL reading must be listed as excluded: %+v", before)
	}

	e.approve(t, "unit_conversion", "GLUCOSE|mg/dL|mmol/L", `{"code":"GLUCOSE","from_unit":"mg/dL","to_unit":"mmol/L","factor":0.0555,"source":"reviewed reference"}`)
	after := e.trend(t, "GLUCOSE", "")
	if len(after.Points) != 2 || len(after.Excluded) != 0 {
		t.Fatalf("with an approved conversion both readings are comparable: %+v", after)
	}
	first := after.Points[0]
	if !first.Converted || first.OriginalUnit != "mg/dL" || first.OriginalValue != 100 || first.Value < 5.54 || first.Value > 5.56 {
		t.Fatalf("converted point keeps the original: %+v", first)
	}
	if after.Points[1].Converted {
		t.Error("a reading already in the canonical unit is not converted")
	}
	if after.Trend.Direction != "stable" && after.Trend.Direction != "falling" {
		t.Errorf("5.55 to 5.4 should not be rising: %+v", after.Trend)
	}
}

func TestLabPrintedRangeBeatsLearnedRange(t *testing.T) {
	e := commandCentre(t, "bm-range", storage()...)
	e.approve(t, "reference_range", "GLUCOSE|mg/dL|any", `{"code":"GLUCOSE","unit":"mg/dL","low":70,"high":99,"source":"approved guideline"}`)
	e.observeFull(t, "GLUCOSE", 105, "mg/dL", ago(20), "VERIFIED", "LAB_DOCUMENT", "")
	e.observeFull(t, "GLUCOSE", 105, "mg/dL", ago(10), "VERIFIED", "LAB_DOCUMENT", "70-110")

	got := e.trend(t, "GLUCOSE", "")
	if len(got.Points) != 2 {
		t.Fatalf("points: %+v", got.Points)
	}
	learned, printed := got.Points[0], got.Points[1]
	if learned.Range == nil || learned.Range.Origin != "approved_reference" || learned.Flag != "above_range" {
		t.Errorf("learned range: %+v", learned)
	}
	if printed.Range == nil || printed.Range.Origin != "lab_report" || printed.Flag != "within_range" {
		t.Errorf("the lab's own range must win: %+v", printed)
	}
}

func TestLearnedRangesRespectSexAndAge(t *testing.T) {
	e := commandCentre(t, "bm-demo", storage()...)
	e.approve(t, "reference_range", "GLUCOSE|mg/dL|female", `{"code":"GLUCOSE","unit":"mg/dL","sex":"female","low":60,"high":90,"source":"female guideline"}`)
	e.observeFull(t, "GLUCOSE", 95, "mg/dL", ago(3), "VERIFIED", "LAB_DOCUMENT", "")

	if p := e.trend(t, "GLUCOSE", "").Points[0]; p.Range != nil || p.Flag != "unknown" {
		t.Fatalf("a sex-specific range must not apply when sex is unknown: %+v", p)
	}
	e.do("PATCH", "/profiles/"+e.profile, `{"sex":"female"}`)
	if p := e.trend(t, "GLUCOSE", "").Points[0]; p.Range == nil || p.Flag != "above_range" {
		t.Fatalf("applies once sex is known: %+v", p)
	}
	e.do("PATCH", "/profiles/"+e.profile, `{"sex":"male"}`)
	if p := e.trend(t, "GLUCOSE", "").Points[0]; p.Range != nil {
		t.Fatalf("another group's range must not apply: %+v", p)
	}
}

func TestUnverifiedValuesAreSeparatedAndDevicesAreLabelled(t *testing.T) {
	e := commandCentre(t, "bm-verify", storage()...)
	e.observeFull(t, "GLUCOSE", 90, "mg/dL", ago(30), "VERIFIED", "LAB_DOCUMENT", "")
	e.observeFull(t, "GLUCOSE", 92, "mg/dL", ago(15), "VERIFIED", "DEVICE_HEALTHKIT", "")
	e.observeFull(t, "GLUCOSE", 300, "mg/dL", ago(2), "PENDING_REVIEW", "LAB_DOCUMENT", "")

	def := e.trend(t, "GLUCOSE", "")
	if len(def.Points) != 2 || def.Unverified != 1 || def.CanonicalUnit != "mg/dL" {
		t.Fatalf("default view is verified only: %+v", def)
	}
	if len(def.Kinds) != 2 || def.Kinds[0] != "device" || def.Kinds[1] != "lab" {
		t.Errorf("lab and device must stay distinguishable: %v", def.Kinds)
	}
	all := e.trend(t, "GLUCOSE", "?include_unverified=true")
	last := all.Points[len(all.Points)-1]
	if len(all.Points) != 3 || last.Verification != "PENDING_REVIEW" {
		t.Fatalf("include_unverified: %+v", all.Points)
	}
	if all.Trend.Points != 2 {
		t.Errorf("an unverified value must never feed the trend: %+v", all.Trend)
	}
}

func TestBiomarkerSummary(t *testing.T) {
	e := commandCentre(t, "bm-summary", storage()...)
	for n, v := range []float64{5.4, 5.8, 6.2} {
		e.observe(t, "HBA1C", "HbA1c", v, "%", ago(300-n*100), "VERIFIED")
	}
	e.observe(t, "GLUCOSE", "Glucose", 99, "mg/dL", ago(1), "PENDING_REVIEW")

	status, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/summary", "")
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	items := data[[]biomarkerSummary](t, env)
	by := map[string]biomarkerSummary{}
	for _, s := range items {
		by[s.BiomarkerCode] = s
	}
	h := by["HBA1C"]
	if h.Latest == nil || h.Latest.Value != 6.2 || h.Previous == nil || *h.Previous != 5.8 || h.Change == nil || *h.Change < 0.39 || *h.Change > 0.41 || h.Trend.Direction != "rising" || h.Readings != 3 {
		t.Fatalf("HbA1c summary: %+v", h)
	}
	if h.Cadence.TypicalGapDays == nil || *h.Cadence.TypicalGapDays != 100 || h.Cadence.Overdue {
		t.Errorf("cadence: %+v", h.Cadence)
	}
	g := by["GLUCOSE"]
	if g.Latest != nil || g.Unverified != 1 {
		t.Errorf("a biomarker with only unverified values has no latest: %+v", g)
	}
}

func TestBiomarkerEndpointsAreOwnerScopedAndValidated(t *testing.T) {
	e := commandCentre(t, "bm-scope", storage()...)
	for _, path := range []string{"/biomarkers/summary", "/biomarkers/GLUCOSE/trend"} {
		if code, _ := e.doAs("intruder", "GET", "/profiles/"+e.profile+path, ""); code != 404 {
			t.Errorf("%s leaked to another owner: %d", path, code)
		}
	}
	if code, _ := e.do("GET", "/profiles/"+e.profile+"/biomarkers/NOPE/trend", ""); code != 404 {
		t.Errorf("unknown biomarker: %d", code)
	}
	if got := e.trend(t, "GLUCOSE", ""); len(got.Points) != 0 || got.DisplayName != "Glucose" {
		t.Errorf("a known biomarker with no data returns an empty series: %+v", got)
	}
}

func TestMalformedRangesAndConversionsAreRejectedAtReview(t *testing.T) {
	e := commandCentre(t, "bm-bad", storage()...)
	ctx := context.Background()
	for kind, payload := range map[string]string{
		"reference_range": `{"code":"GLUCOSE","unit":"mg/dL","low":99,"high":70,"source":"x"}`,
		"unit_conversion": `{"code":"GLUCOSE","from_unit":"a","to_unit":"a","factor":1,"source":"x"}`,
	} {
		tx, _ := e.a.db.Pool.Begin(ctx)
		if applyKnowledge(ctx, tx, kind, "00000000-0000-0000-0000-000000000000", []byte(payload), "r", "", nil) == nil {
			t.Errorf("%s accepted: %s", kind, payload)
		}
		_ = tx.Rollback(ctx)
	}
	for _, payload := range []string{
		`{"code":"GLUCOSE","unit":"mg/dL","source":"x"}`,
		`{"code":"GLUCOSE","unit":"mg/dL","sex":"other","low":1,"source":"x"}`,
		fmt.Sprintf(`{"code":"GLUCOSE","unit":"mg/dL","low":1,"source":%q}`, " "),
	} {
		tx, _ := e.a.db.Pool.Begin(ctx)
		if applyKnowledge(ctx, tx, "reference_range", "00000000-0000-0000-0000-000000000000", []byte(payload), "r", "", nil) == nil {
			t.Errorf("accepted: %s", payload)
		}
		_ = tx.Rollback(ctx)
	}
}
