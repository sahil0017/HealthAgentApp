package api

import (
	"context"
	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/modules/biomarkers"
	"cortex/backend/internal/modules/commandcentre"
	"fmt"
	"strings"
	"testing"
)

// approve pushes a proposal through the same reviewed path the research agent uses.
func (e *ccEnv) approve(t *testing.T, kind, key, payload string) {
	t.Helper()
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	ctx := context.Background()
	var id string
	if err := e.a.DB.Pool.QueryRow(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload) VALUES($1::text,$2::text,$3::text::jsonb) ON CONFLICT(kind,entry_key) DO UPDATE SET payload=EXCLUDED.payload,status='pending' RETURNING id::text`, kind, key, payload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if status, _ := e.doAs("reviewer-1", "POST", "/knowledge/entries/"+id+"/approve", "{}"); status != 200 {
		t.Fatalf("approve %s: status %d", kind, status)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM reference_ranges WHERE knowledge_entry_id=$1::uuid`, `DELETE FROM unit_conversions WHERE knowledge_entry_id=$1::uuid`, `DELETE FROM knowledge_entries WHERE id=$1::uuid`} {
			_, _ = e.a.DB.Pool.Exec(context.Background(), q, id)
		}
	})
}

func (e *ccEnv) observeFull(t *testing.T, code string, value float64, unit, on, status, sourceType, labRange string) {
	t.Helper()
	if _, err := e.a.DB.Pool.Exec(context.Background(), `INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,verification_status,source_type,reference_range) VALUES($1,$2,$3,$3,$4,$5,$6::text::date,$7,$8,NULLIF($9::text,''))`, e.profile, e.owner, code, value, unit, on, status, sourceType, labRange); err != nil {
		t.Fatal(err)
	}
}

func (e *ccEnv) trend(t *testing.T, code, query string) biomarker.Trend {
	t.Helper()
	status, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/"+code+"/trend"+query, "")
	if status != 200 {
		t.Fatalf("trend status %d", status)
	}
	return data[biomarker.Trend](t, env)
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
	items := data[[]biomarkers.BiomarkerSummary](t, env)
	by := map[string]biomarkers.BiomarkerSummary{}
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
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	ctx := context.Background()
	rejected := func(kind, payload string) bool {
		var id string
		if err := e.a.DB.Pool.QueryRow(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload) VALUES($1::text,$2::text,$3::text::jsonb) RETURNING id::text`, kind, "bad-"+payload, payload).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = e.a.DB.Pool.Exec(ctx, `DELETE FROM knowledge_entries WHERE id=$1::uuid`, id) })
		status, _ := e.doAs("reviewer-1", "POST", "/knowledge/entries/"+id+"/approve", "{}")
		return status != 200
	}
	for kind, payload := range map[string]string{
		"reference_range": `{"code":"GLUCOSE","unit":"mg/dL","low":99,"high":70,"source":"x"}`,
		"unit_conversion": `{"code":"GLUCOSE","from_unit":"a","to_unit":"a","factor":1,"source":"x"}`,
	} {
		if !rejected(kind, payload) {
			t.Errorf("%s accepted: %s", kind, payload)
		}
	}
	for _, payload := range []string{
		`{"code":"GLUCOSE","unit":"mg/dL","source":"x"}`,
		`{"code":"GLUCOSE","unit":"mg/dL","sex":"other","low":1,"source":"x"}`,
		fmt.Sprintf(`{"code":"GLUCOSE","unit":"mg/dL","low":1,"source":%q}`, " "),
	} {
		if !rejected("reference_range", payload) {
			t.Errorf("accepted: %s", payload)
		}
	}
}

func (e *ccEnv) attentionKinds(t *testing.T) map[string]commandcentre.AttentionItem {
	t.Helper()
	code, env := e.do("GET", "/profiles/"+e.profile+"/overview", "")
	if code != 200 {
		t.Fatalf("overview %d", code)
	}
	out := map[string]commandcentre.AttentionItem{}
	for _, item := range data[commandcentre.Overview](t, env).Attention {
		out[item.Kind] = item
	}
	return out
}

func TestOverviewFlagsOutOfRangeFromVerifiedReadingsOnly(t *testing.T) {
	e := commandCentre(t, "bm-attention", storage()...)
	for _, c := range []string{"GLUCOSE", "LDL", "FERRITIN"} {
		_, _ = e.a.DB.Pool.Exec(context.Background(), `INSERT INTO biomarker_catalog(code,display_name) VALUES($1,$1) ON CONFLICT(code) DO NOTHING`, c)
	}
	e.observeFull(t, "GLUCOSE", 130, "mg/dL", ago(3), "VERIFIED", "LAB_DOCUMENT", "70-99")
	e.observeFull(t, "LDL", 300, "mg/dL", ago(3), "PENDING_REVIEW", "LAB_DOCUMENT", "0-100")
	e.observeFull(t, "FERRITIN", 50, "ng/mL", ago(3), "VERIFIED", "LAB_DOCUMENT", "30-300")

	kinds := e.attentionKinds(t)
	item, ok := kinds["out_of_range"]
	if !ok || item.RefID != "GLUCOSE" || item.Severity != "info" || !strings.Contains(item.Title, "above") {
		t.Fatalf("glucose should be flagged once, softly: %+v", kinds)
	}
	_, env := e.do("GET", "/profiles/"+e.profile+"/overview", "")
	n := 0
	for _, a := range data[commandcentre.Overview](t, env).Attention {
		if a.Kind == "out_of_range" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("unverified and in-range values must not be flagged, got %d items", n)
	}
}

func TestOverviewFlagsGoalsWithoutVerifiedData(t *testing.T) {
	e := commandCentre(t, "bm-goal-nodata", storage()...)
	e.do("POST", "/profiles/"+e.profile+"/goals", `{"biomarker_code":"HBA1C","unit":"%","target_max":5.7}`)
	if _, ok := e.attentionKinds(t)["goal_without_data"]; !ok {
		t.Fatal("a goal with no readings should be flagged")
	}
	e.observe(t, "HBA1C", "HbA1c", 5.2, "%", ago(2), "VERIFIED")
	if _, ok := e.attentionKinds(t)["goal_without_data"]; ok {
		t.Fatal("a goal with a verified reading should not be flagged")
	}
}

func TestCategoriesComeFromReviewedCatalogEntriesAndGroupTheSummary(t *testing.T) {
	e := commandCentre(t, "bm-cat", storage()...)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	e.approve(t, "biomarker", "GLUCOSE", `{"code":"GLUCOSE","display_name":"Glucose","category":"glucose","supported_units":["mg/dL"]}`)
	t.Cleanup(func() {
		_, _ = e.a.DB.Pool.Exec(context.Background(), `UPDATE biomarker_catalog SET category=NULL WHERE code='GLUCOSE'`)
	})
	e.observe(t, "GLUCOSE", "Glucose", 95, "mg/dL", ago(10), "VERIFIED")
	e.observe(t, "HBA1C", "HbA1c", 5.4, "%", ago(10), "VERIFIED")

	code, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/summary", "")
	if code != 200 {
		t.Fatalf("summary %d", code)
	}
	cats := map[string]string{}
	for _, s := range data[[]biomarkers.BiomarkerSummary](t, env) {
		cats[s.BiomarkerCode] = s.Category
	}
	if cats["GLUCOSE"] != "glucose" || cats["HBA1C"] != "glucose" {
		t.Fatalf("categories %v", cats)
	}
}

func TestInvalidCategoryIsRejectedAtReview(t *testing.T) {
	e := commandCentre(t, "bm-badcat", storage()...)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	var id string
	if err := e.a.DB.Pool.QueryRow(context.Background(), `INSERT INTO knowledge_entries(kind,entry_key,payload) VALUES('biomarker','BADCAT','{"code":"BADCAT","display_name":"Bad","category":"astrology"}') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.a.DB.Pool.Exec(context.Background(), `DELETE FROM knowledge_entries WHERE id=$1::uuid`, id)
	})
	if status, _ := e.doAs("reviewer-1", "POST", "/knowledge/entries/"+id+"/approve", "{}"); status == 200 {
		t.Fatal("unknown category accepted")
	}
}

func TestCompareShowsChangeAcrossDatesInOneUnitAndIgnoresUnverified(t *testing.T) {
	e := commandCentre(t, "bm-compare", storage()...)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	e.approve(t, "unit_conversion", "GLUCOSE-mmol", `{"code":"GLUCOSE","from_unit":"mmol/L","to_unit":"mg/dL","factor":18.016,"source":"test"}`)
	e.approve(t, "reference_range", "GLUCOSE-r", `{"code":"GLUCOSE","unit":"mg/dL","low":70,"high":99,"source":"test"}`)
	e.observe(t, "GLUCOSE", "Glucose", 130, "mg/dL", ago(400), "VERIFIED")
	e.observe(t, "GLUCOSE", "Glucose", 5.0, "mmol/L", ago(20), "VERIFIED")
	e.observe(t, "GLUCOSE", "Glucose", 400, "mg/dL", ago(5), "PENDING_REVIEW")

	code, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/compare?from="+ago(300)+"&to="+ahead(0), "")
	if code != 200 {
		t.Fatalf("compare %d", code)
	}
	changes := data[[]biomarker.Change](t, env)
	if len(changes) != 1 {
		t.Fatalf("%+v", changes)
	}
	c := changes[0]
	if c.Status != "returned_to_range" || c.Unit != "mmol/L" || c.To == nil || c.To.Value != 5 || c.Change == nil || *c.Change > -2 || *c.Change < -2.5 {
		t.Fatalf("unexpected change %+v to=%+v", c, c.To)
	}
	for _, q := range []string{"", "?from=nope", "?from=" + ago(1) + "&to=" + ago(9)} {
		if code, _ := e.do("GET", "/profiles/"+e.profile+"/biomarkers/compare"+q, ""); code != 400 {
			t.Errorf("%q: want 400, got %d", q, code)
		}
	}
	if code, _ := e.doAs("someone-else", "GET", "/profiles/"+e.profile+"/biomarkers/compare?from="+ago(300), ""); code != 404 {
		t.Errorf("another owner must not compare: %d", code)
	}
}

func TestCoverageReportsMissingAndStalePanels(t *testing.T) {
	e := commandCentre(t, "bm-coverage", storage()...)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	e.approve(t, "biomarker", "GLUCOSE", `{"code":"GLUCOSE","display_name":"Glucose","category":"glucose","supported_units":["mg/dL"]}`)
	t.Cleanup(func() {
		_, _ = e.a.DB.Pool.Exec(context.Background(), `UPDATE biomarker_catalog SET category=NULL WHERE code='GLUCOSE'`)
	})
	e.observe(t, "GLUCOSE", "Glucose", 95, "mg/dL", ago(400), "VERIFIED")

	code, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/coverage", "")
	if code != 200 {
		t.Fatalf("coverage %d", code)
	}
	got := map[string]biomarker.PanelCoverage{}
	for _, p := range data[[]biomarker.PanelCoverage](t, env) {
		got[p.Category] = p
	}
	if got["glucose"].Status != "stale" || got["lipid"].Status != "never_measured" || got["cbc"].Status != "never_measured" {
		t.Fatalf("%+v", got)
	}
	if code, _ := e.doAs("someone-else", "GET", "/profiles/"+e.profile+"/biomarkers/coverage", ""); code != 404 {
		t.Errorf("another owner must not read coverage: %d", code)
	}
}

func TestReviewerCanAssignTheCategoryWhenApproving(t *testing.T) {
	e := commandCentre(t, "bm-revcat", storage()...)
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "reviewer-1")
	var id string
	if err := e.a.DB.Pool.QueryRow(context.Background(), `INSERT INTO knowledge_entries(kind,entry_key,payload) VALUES('biomarker','LDLX','{"code":"LDLX","display_name":"LDL test"}') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.a.DB.Pool.Exec(context.Background(), `DELETE FROM biomarker_catalog WHERE code='LDLX'`)
		_, _ = e.a.DB.Pool.Exec(context.Background(), `DELETE FROM knowledge_entries WHERE id=$1::uuid`, id)
	})
	if status, _ := e.doAs("reviewer-1", "POST", "/knowledge/entries/"+id+"/approve", `{"category":"astrology"}`); status != 400 {
		t.Fatalf("bad category: %d", status)
	}
	if status, _ := e.doAs("reviewer-1", "POST", "/knowledge/entries/"+id+"/approve", `{"category":"lipid"}`); status != 200 {
		t.Fatalf("approve: %d", status)
	}
	var category string
	if err := e.a.DB.Pool.QueryRow(context.Background(), `SELECT category FROM biomarker_catalog WHERE code='LDLX'`).Scan(&category); err != nil || category != "lipid" {
		t.Fatalf("category %q err %v", category, err)
	}
}

func TestResultsAreGroupedUnderTheirReportWithChangeSincePrevious(t *testing.T) {
	e := commandCentre(t, "bm-byreport", storage()...)
	ctx := context.Background()
	docs := []string{}
	for i, day := range []int{40, 5} {
		var id string
		err := e.a.DB.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status,document_type,extraction) VALUES($1,$2,$3,$4,'application/pdf',10,'approved','LAB_REPORT',$5::jsonb) RETURNING id::text`,
			e.profile, e.owner, "k/"+e.profile+"/"+string(rune('a'+i)), "report.pdf", `{"ai":{"provider":"Orange Health","report_date":"`+ago(day)+`"}}`).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, id)
		_, err = e.a.DB.Pool.Exec(ctx, `INSERT INTO observations(profile_id,owner_id,document_id,biomarker_code,name,value,unit,observed_at,verification_status) VALUES($1,$2,$3,'GLUCOSE','Glucose',$4,'mg/dL',$5::date,'VERIFIED')`, e.profile, e.owner, id, 90+i*20, ago(day))
		if err != nil {
			t.Fatal(err)
		}
	}
	code, env := e.do("GET", "/profiles/"+e.profile+"/biomarkers/by-report", "")
	if code != 200 {
		t.Fatalf("by-report %d", code)
	}
	groups := data[[]biomarkers.ReportGroup](t, env)
	if len(groups) != 2 || groups[0].DocumentID != docs[1] || groups[0].Title != "Orange Health" {
		t.Fatalf("want newest report first, titled by its lab, got %+v", groups)
	}
	r := groups[0].Results[0]
	if r.Change == nil || *r.Change != 20 || groups[1].Results[0].Change != nil {
		t.Fatalf("the later report should show +20 against the earlier one: %+v / %+v", r, groups[1].Results[0])
	}
}
