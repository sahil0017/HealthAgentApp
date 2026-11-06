package agent

import (
	"context"
	"strings"
	"testing"
)

func TestWellbeingExcludesUnratedLabsAndUnmeasurableGoals(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code := "TEST_" + strings.ToUpper(strings.ReplaceAll(f.profile, "-", ""))
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO biomarker_catalog(code,display_name,synonyms,supported_units) VALUES($1,'Unrated test','{}','{unit}')`, code); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.c.DB.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, f.profile)
		_, _ = f.c.DB.Pool.Exec(ctx, `DELETE FROM biomarker_catalog WHERE code=$1`, code)
	})
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,source_type,verification_status) VALUES($1,'owner-a',$2,'Unrated test',10,'unit','LAB_DOCUMENT','VERIFIED')`, f.profile, code); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO goals(profile_id,owner_id,biomarker_code,title,target_max,unit) VALUES($1,'owner-a',$2,'Other unit target',12,'incompatible')`, f.profile, code); err != nil {
		t.Fatal(err)
	}
	status, out := f.do("GET", "/agent/wellbeing?day=2026-11-05", "")
	if status != 200 {
		t.Fatalf("wellbeing: %d %v", status, out)
	}
	d := data(out)
	labs := d["labs"].(map[string]any)
	care := d["care"].(map[string]any)
	if labs["unrated"] != float64(1) || labs["in_range"] != float64(0) || care["assessed_goals"] != float64(0) || len(d["parts"].([]any)) != 0 {
		t.Fatalf("unrated or incompatible results were scored: %v", d)
	}
	f.seedHighGlucose()
	status, out = f.do("GET", "/agent/wellbeing?day=2026-11-05", "")
	if status != 200 {
		t.Fatalf("mixed wellbeing: %d %v", status, out)
	}
	d = data(out)
	labs = d["labs"].(map[string]any)
	if labs["out_of_range"] != float64(1) || labs["unrated"] != float64(1) || d["score"] != float64(0) {
		t.Fatalf("unrated result inflated the score: %v", d)
	}
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO unit_conversions(biomarker_code,from_unit,to_unit,factor,source,approved_by) VALUES($1,'unit','double',2,'test','test')`, code); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO goals(profile_id,owner_id,biomarker_code,title,target_max,unit)
		VALUES($1,'owner-a',$2,'First target',9,'unit'),($1,'owner-a',$2,'Second target',18,'double')`, f.profile, code); err != nil {
		t.Fatal(err)
	}
	status, out = f.do("GET", "/agent/wellbeing?day=2026-11-05", "")
	if status != 200 {
		t.Fatalf("converted wellbeing: %d %v", status, out)
	}
	d = data(out)
	care = d["care"].(map[string]any)
	if care["assessed_goals"] != float64(2) || care["on_target"] != float64(0) {
		t.Fatalf("incorrect goal assessment: %v", care)
	}
	found := map[string]bool{}
	for _, task := range d["tasks"].([]any) {
		t := task.(map[string]any)
		found[t["why"].(string)] = true
	}
	if !found["Latest 10 unit, target up to 9."] || !found["Latest 20 double, target up to 18."] {
		t.Fatalf("goal tasks used wrong units: %v", found)
	}
}

func TestLabFallbackDoesNotCallUnknownResultsNormal(t *testing.T) {
	got := labFallback([]labLine{{Name: "Unrated", Flag: "unknown"}})
	if got.Headline != "No results have a reference range for comparison. 1 without a range." || len(got.Items) != 0 {
		t.Fatalf("unexpected insight: %+v", got)
	}
}
