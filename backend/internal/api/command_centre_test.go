package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cortex/backend/internal/domain/health"
	"cortex/backend/internal/modules/commandcentre"
	"cortex/backend/internal/modules/identity"
	"cortex/backend/internal/platform/core"
)

type ccEnv struct {
	a       *core.Core
	h       http.Handler
	profile string
	owner   string
}

func commandCentre(t *testing.T, ownerID string, consents ...string) *ccEnv {
	t.Helper()
	a, r := newAPI(t, ownerID, nil)
	ctx := context.Background()
	db := a.DB
	// The catalog is learned, not shipped, so tests bring their own entries.
	for _, b := range []struct{ code, name, units string }{{"HBA1C", "Hemoglobin A1c", "{%,mmol/mol}"}, {"GLUCOSE", "Glucose", "{mg/dL,mmol/L}"}} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO biomarker_catalog(code,display_name,supported_units) VALUES($1,$2,$3::text[]) ON CONFLICT(code) DO UPDATE SET display_name=EXCLUDED.display_name,supported_units=EXCLUDED.supported_units`, b.code, b.name, b.units); err != nil {
			t.Fatal(err)
		}
	}
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name,timezone) VALUES($1,'Ada','Asia/Kolkata') RETURNING id::text`, ownerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, id) })
	for _, purpose := range consents {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,$2,true)`, id, purpose); err != nil {
			t.Fatal(err)
		}
	}
	return &ccEnv{a: a, h: r, profile: id, owner: ownerID}
}

func (e *ccEnv) do(method, path, body string) (int, map[string]json.RawMessage) {
	return e.doAs("", method, path, body)
}

func (e *ccEnv) doAs(who, method, path, body string) (int, map[string]json.RawMessage) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != "" {
		req.Header.Set("X-Test-Owner", who)
	}
	e.h.ServeHTTP(w, req)
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w.Code, envelope
}

func data[T any](t *testing.T, envelope map[string]json.RawMessage) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(envelope["data"], &out); err != nil {
		t.Fatalf("decode %s: %v", envelope["data"], err)
	}
	return out
}

func (e *ccEnv) observe(t *testing.T, code, name string, value float64, unit, on, status string) {
	t.Helper()
	_, err := e.a.DB.Pool.Exec(context.Background(), `INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,verification_status) VALUES($1,$2,$3,$4,$5,$6,$7::date,$8)`, e.profile, e.owner, code, name, value, unit, on, status)
	if err != nil {
		t.Fatal(err)
	}
}

func ago(days int) string   { return time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02") }
func ahead(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02") }

func storage() []string { return []string{"medical_record_storage", "document_processing"} }

func TestWritesNeedStorageConsentAndReadsDoNot(t *testing.T) {
	e := commandCentre(t, "cc-noconsent")
	for _, req := range []struct{ path, body string }{
		{"/history", `{"kind":"allergy","title":"Penicillin"}`},
		{"/follow-ups", `{"kind":"investigation","title":"Repeat HbA1c"}`},
		{"/goals", `{"biomarker_code":"HBA1C","unit":"%","target_max":5.7}`},
	} {
		if code, _ := e.do("POST", "/profiles/"+e.profile+req.path, req.body); code != 403 {
			t.Errorf("%s: expected 403 without consent, got %d", req.path, code)
		}
	}
	if code, _ := e.do("GET", "/profiles/"+e.profile+"/history", ""); code != 200 {
		t.Errorf("reading is allowed, got %d", code)
	}
}

func TestHistoryLifecycleValidationAndOwnership(t *testing.T) {
	e := commandCentre(t, "cc-history", storage()...)
	base := "/profiles/" + e.profile + "/history"
	for name, body := range map[string]string{
		"bad kind":       `{"kind":"wizardry","title":"x"}`,
		"blank title":    `{"kind":"allergy","title":"  "}`,
		"bad date":       `{"kind":"allergy","title":"x","started_on":"03/02/2026"}`,
		"end before":     `{"kind":"condition","title":"x","started_on":"2026-03-01","ended_on":"2026-02-01"}`,
		"unknown field":  `{"kind":"allergy","title":"x","extra":1}`,
		"missing fields": `{}`,
	} {
		if code, _ := e.do("POST", base, body); code != 400 {
			t.Errorf("%s: expected 400, got %d", name, code)
		}
	}
	code, env := e.do("POST", base, `{"kind":"condition","title":" Asthma ","notes":"mild","started_on":"2015-04-01"}`)
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	entry := data[commandcentre.HistoryEntry](t, env)
	if entry.Title != "Asthma" || entry.Status != "active" || *entry.StartedOn != "2015-04-01" {
		t.Fatalf("unexpected entry %+v", entry)
	}
	code, env = e.do("PATCH", "/history/"+entry.ID, `{"status":"resolved","ended_on":"2020-01-01"}`)
	if code != 200 || data[commandcentre.HistoryEntry](t, env).Status != "resolved" {
		t.Fatalf("update: %d", code)
	}
	if code, _ := e.do("PATCH", "/history/"+entry.ID, `{"ended_on":"2001-01-01"}`); code != 400 {
		t.Fatalf("an end date before the start must be rejected, got %d", code)
	}
	e.do("POST", base, `{"kind":"allergy","title":"Penicillin"}`)
	_, env = e.do("GET", base+"?kind=allergy", "")
	if list := data[[]commandcentre.HistoryEntry](t, env); len(list) != 1 || list[0].Title != "Penicillin" {
		t.Fatalf("kind filter: %+v", list)
	}
	if code, _ := e.doAs("intruder", "PATCH", "/history/"+entry.ID, `{"title":"hijack"}`); code != 404 {
		t.Fatalf("another owner must not edit, got %d", code)
	}
	if code, _ := e.doAs("intruder", "DELETE", "/history/"+entry.ID, ""); code != 404 {
		t.Fatalf("another owner must not delete, got %d", code)
	}
	if code, _ := e.do("DELETE", "/history/"+entry.ID, ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := e.do("DELETE", "/history/"+entry.ID, ""); code != 404 {
		t.Fatalf("second delete should 404, got %d", code)
	}
	if n := count(t, e.a, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action IN ('history.created','history.updated','history.deleted')`, e.profile); n != 4 {
		t.Fatalf("history changes must be audited (2 creates, 1 update, 1 delete), got %d", n)
	}
}

func TestFollowUpCompletionAndReopening(t *testing.T) {
	e := commandCentre(t, "cc-followup", storage()...)
	code, env := e.do("POST", "/profiles/"+e.profile+"/follow-ups", fmt.Sprintf(`{"kind":"investigation","title":"Repeat HbA1c","due_on":%q}`, ahead(10)))
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	item := data[commandcentre.FollowUp](t, env)
	if item.Status != "open" || item.CompletedAt != nil {
		t.Fatalf("new follow-up: %+v", item)
	}
	_, env = e.do("PATCH", "/follow-ups/"+item.ID, `{"status":"done"}`)
	done := data[commandcentre.FollowUp](t, env)
	if done.Status != "done" || done.CompletedAt == nil {
		t.Fatalf("done should stamp completed_at: %+v", done)
	}
	_, env = e.do("PATCH", "/follow-ups/"+item.ID, `{"notes":"booked"}`)
	if still := data[commandcentre.FollowUp](t, env); still.CompletedAt == nil || still.Notes != "booked" {
		t.Fatalf("editing notes must keep the completion time: %+v", still)
	}
	_, env = e.do("PATCH", "/follow-ups/"+item.ID, `{"status":"open"}`)
	if reopened := data[commandcentre.FollowUp](t, env); reopened.CompletedAt != nil {
		t.Fatalf("reopening must clear completed_at: %+v", reopened)
	}
	if code, _ := e.do("GET", "/profiles/"+e.profile+"/follow-ups?status=bogus", ""); code != 400 {
		t.Fatalf("bad status filter: %d", code)
	}
	if code, _ := e.doAs("intruder", "PATCH", "/follow-ups/"+item.ID, `{"status":"done"}`); code != 404 {
		t.Fatalf("another owner must not edit, got %d", code)
	}
}

func TestGoalValidationAndProgressUsesOnlyVerifiedValues(t *testing.T) {
	e := commandCentre(t, "cc-goal", storage()...)
	base := "/profiles/" + e.profile + "/goals"
	for name, body := range map[string]string{
		"unknown biomarker": `{"biomarker_code":"NOPE","unit":"%","target_max":5}`,
		"no target":         `{"biomarker_code":"HBA1C","unit":"%"}`,
		"min above max":     `{"biomarker_code":"HBA1C","unit":"%","target_min":9,"target_max":5}`,
		"unsupported unit":  `{"biomarker_code":"HBA1C","unit":"furlongs","target_max":5}`,
		"no unit":           `{"biomarker_code":"HBA1C","target_max":5}`,
	} {
		if code, _ := e.do("POST", base, body); code != 400 {
			t.Errorf("%s: expected 400, got %d", name, code)
		}
	}
	e.observe(t, "HBA1C", "HbA1c", 6.8, "%", ago(120), "VERIFIED")
	e.observe(t, "HBA1C", "HbA1c", 6.1, "%", ago(30), "VERIFIED")
	e.observe(t, "HBA1C", "HbA1c", 4.0, "%", ago(1), "PENDING_REVIEW")
	code, env := e.do("POST", base, `{"biomarker_code":"hba1c","unit":"%","target_max":5.7}`)
	if code != 201 {
		t.Fatalf("create: %d %s", code, env["error"])
	}
	g := data[commandcentre.GoalView](t, env)
	if g.Title != "Hemoglobin A1c" || g.Progress.Status != "above_target" || g.Progress.Trend != "improving" || *g.Progress.LatestValue != 6.1 {
		t.Fatalf("progress must ignore the unverified 4.0: %+v", g)
	}
	_, env = e.do("PATCH", "/goals/"+g.ID, `{"target_max":6.5}`)
	if g = data[commandcentre.GoalView](t, env); g.Progress.Status != "on_target" {
		t.Fatalf("raising the target should make 6.1 on target: %+v", g)
	}
	if code, _ := e.do("PATCH", "/goals/"+g.ID, `{"biomarker_code":"GLUCOSE"}`); code != 400 {
		t.Fatalf("changing the biomarker must be rejected, got %d", code)
	}
	if code, _ := e.do("PATCH", "/goals/"+g.ID, `{"target_min":99}`); code != 400 {
		t.Fatalf("a minimum above the maximum must be rejected, got %d", code)
	}
	_, env = e.do("PATCH", "/goals/"+g.ID, `{"status":"archived"}`)
	_, env = e.do("GET", base+"?status=active", "")
	if list := data[[]commandcentre.GoalView](t, env); len(list) != 0 {
		t.Fatalf("archived goals are not active: %+v", list)
	}
	if code, _ := e.doAs("intruder", "DELETE", "/goals/"+g.ID, ""); code != 404 {
		t.Fatalf("another owner must not delete, got %d", code)
	}
}

func TestOverviewBringsTheWholePictureTogether(t *testing.T) {
	e := commandCentre(t, "cc-overview", storage()...)
	ctx := context.Background()
	// A usual 30-day cadence that went quiet 200 days ago.
	for i, v := range []float64{5.2, 5.4, 5.6, 5.9} {
		e.observe(t, "HBA1C", "HbA1c", v, "%", ago(290-30*i), "VERIFIED")
	}
	e.observe(t, "GLUCOSE", "Glucose", 99, "mg/dL", ago(3), "VERIFIED")
	e.observe(t, "GLUCOSE", "Glucose", 90, "mg/dL", ago(60), "VERIFIED")
	e.observe(t, "GLUCOSE", "Glucose", 110, "mg/dL", ago(2), "PENDING_REVIEW")
	for _, status := range []string{"review_required", "failed", "approved"} {
		if _, err := e.a.DB.Pool.Exec(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1,$2,$3,'r.pdf','application/pdf',1,$4)`, e.profile, e.owner, "cc/"+status+e.profile, status); err != nil {
			t.Fatal(err)
		}
	}
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", fmt.Sprintf(`{"kind":"appointment","title":"Overdue visit","due_on":%q}`, ago(5)))
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", fmt.Sprintf(`{"kind":"investigation","title":"Soon test","due_on":%q}`, ahead(3)))
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", fmt.Sprintf(`{"kind":"other","title":"Far away","due_on":%q}`, ahead(60)))
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", `{"kind":"other","title":"No date"}`)
	e.do("POST", "/profiles/"+e.profile+"/history", `{"kind":"condition","title":"Asthma"}`)
	e.do("POST", "/profiles/"+e.profile+"/goals", `{"biomarker_code":"HBA1C","unit":"%","target_max":5.7}`)

	code, env := e.do("GET", "/profiles/"+e.profile+"/overview", "")
	if code != 200 {
		t.Fatalf("overview: %d %s", code, env["error"])
	}
	o := data[commandcentre.Overview](t, env)
	if o.Profile.DisplayName != "Ada" || o.Summary.Documents != 3 || o.Summary.Observations != 7 || o.Summary.BiomarkersTraced != 2 || o.Summary.OpenFollowUps != 4 || o.Summary.ActiveGoals != 1 {
		t.Fatalf("summary: %+v", o.Summary)
	}
	kinds := map[string]bool{}
	for _, item := range o.Attention {
		kinds[item.Kind] = true
	}
	for _, want := range []string{"review", "failed_document", "unverified", "overdue_follow_up", "measurement_gap", "goal_off_target"} {
		if !kinds[want] {
			t.Errorf("attention is missing %q: %+v", want, o.Attention)
		}
	}
	if kinds["consent"] {
		t.Error("consent was granted, so no consent prompt")
	}
	if o.Attention[0].Severity != "action" {
		t.Errorf("actions must come first: %+v", o.Attention)
	}
	if len(o.Reminders) != 2 || o.Reminders[0].Title != "Overdue visit" || o.Reminders[1].Title != "Soon test" {
		t.Fatalf("reminders should be the overdue and the soon items only: %+v", o.Reminders)
	}
	if o.NextFollowUp == nil || o.NextFollowUp.Title != "Soon test" {
		t.Fatalf("next follow-up should skip overdue ones: %+v", o.NextFollowUp)
	}
	var glucose *health.ChangeSummary
	for i := range o.RecentChanges {
		if o.RecentChanges[i].BiomarkerCode == "GLUCOSE" {
			glucose = &o.RecentChanges[i]
		}
	}
	if glucose == nil || glucose.Value != 99 || glucose.Change == nil || *glucose.Change != 9 {
		t.Fatalf("glucose change must use verified readings only: %+v", glucose)
	}
	if o.RecentChanges[0].BiomarkerCode != "GLUCOSE" {
		t.Errorf("most recently measured first: %+v", o.RecentChanges)
	}
	if len(o.Goals) != 1 || o.Goals[0].Progress.Status != "above_target" {
		t.Fatalf("goals: %+v", o.Goals)
	}
	if o.Completeness.Score != 57 {
		t.Fatalf("completeness: %+v", o.Completeness)
	}
}

func TestOverviewForAnEmptyProfileAsksForPermissionsFirst(t *testing.T) {
	e := commandCentre(t, "cc-empty")
	_, env := e.do("GET", "/profiles/"+e.profile+"/overview", "")
	o := data[commandcentre.Overview](t, env)
	if len(o.Attention) != 1 || o.Attention[0].Kind != "consent" {
		t.Fatalf("attention: %+v", o.Attention)
	}
	if o.Reminders == nil || o.RecentChanges == nil || o.Goals == nil {
		t.Fatal("lists must be empty arrays, not null, so clients can decode them")
	}
	if o.Completeness.Score != 0 || o.NextFollowUp != nil {
		t.Fatalf("empty commandcentre.Overview: %+v", o)
	}
	if code, _ := e.doAs("intruder", "GET", "/profiles/"+e.profile+"/overview", ""); code != 404 {
		t.Fatalf("another owner must not read the commandcentre.Overview, got %d", code)
	}
}

func TestTimelineMixesSourcesNewestFirstWithFiltersAndPaging(t *testing.T) {
	e := commandCentre(t, "cc-timeline", storage()...)
	e.observe(t, "GLUCOSE", "Glucose", 95, "mg/dL", ago(10), "VERIFIED")
	e.do("POST", "/profiles/"+e.profile+"/history", fmt.Sprintf(`{"kind":"surgery","title":"Appendectomy","started_on":%q}`, ago(400)))
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", fmt.Sprintf(`{"kind":"appointment","title":"Check-up","due_on":%q}`, ahead(5)))

	_, env := e.do("GET", "/profiles/"+e.profile+"/timeline", "")
	items := data[[]commandcentre.TimelineItem](t, env)
	if len(items) != 3 || items[0].Kind != "follow_up" || items[1].Kind != "observation" || items[2].Kind != "history" {
		t.Fatalf("order: %+v", items)
	}
	if !strings.Contains(items[1].Title, "Glucose 95 mg/dL") {
		t.Fatalf("observation title: %q", items[1].Title)
	}
	_, env = e.do("GET", "/profiles/"+e.profile+"/timeline?kind=history,observation", "")
	if filtered := data[[]commandcentre.TimelineItem](t, env); len(filtered) != 2 {
		t.Fatalf("filter: %+v", filtered)
	}
	_, env = e.do("GET", "/profiles/"+e.profile+"/timeline?limit=1&offset=1", "")
	if paged := data[[]commandcentre.TimelineItem](t, env); len(paged) != 1 || paged[0].Kind != "observation" {
		t.Fatalf("paging: %+v", paged)
	}
	if code, _ := e.do("GET", "/profiles/"+e.profile+"/timeline?kind=secrets", ""); code != 400 {
		t.Fatalf("unknown kind: %d", code)
	}
	if code, _ := e.do("GET", "/profiles/"+e.profile+"/timeline?limit=0", ""); code != 400 {
		t.Fatalf("bad paging: %d", code)
	}
}

func TestExportAndDeletionCoverCommandCentreData(t *testing.T) {
	e := commandCentre(t, "cc-governance", storage()...)
	e.do("POST", "/profiles/"+e.profile+"/history", `{"kind":"allergy","title":"Penicillin"}`)
	e.do("POST", "/profiles/"+e.profile+"/follow-ups", `{"kind":"other","title":"Call GP"}`)
	e.do("POST", "/profiles/"+e.profile+"/goals", `{"biomarker_code":"GLUCOSE","unit":"mg/dL","target_max":99}`)

	_, env := e.do("GET", "/profiles/"+e.profile+"/export", "")
	var exported map[string][]map[string]any
	raw, _ := json.Marshal(env["data"])
	_ = json.Unmarshal(raw, &exported)
	for _, key := range []string{"history_entries", "goals", "follow_ups"} {
		if len(exported[key]) != 1 {
			t.Errorf("export should include %s: %v", key, exported[key])
		}
	}
	if code, _ := e.do("DELETE", "/profiles/"+e.profile, `{"confirm":"DELETE"}`); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	for _, table := range []string{"history_entries", "goals", "follow_ups"} {
		if n := count(t, e.a, `SELECT count(*) FROM `+table+` WHERE profile_id=$1`, e.profile); n != 0 {
			t.Errorf("%s survived profile deletion", table)
		}
	}
}

func TestProfileHealthDetails(t *testing.T) {
	e := commandCentre(t, "cc-details", storage()...)
	path := "/profiles/" + e.profile
	for _, bad := range []string{
		fmt.Sprintf(`{"date_of_birth":%q}`, ahead(3)),
		`{"date_of_birth":"1800-01-01"}`,
		`{"date_of_birth":"not-a-date"}`,
		`{"sex":"other"}`,
		`{"blood_group":"C+"}`,
		`{"height_cm":12}`,
		`{"height_cm":400}`,
	} {
		if code, _ := e.do("PATCH", path, bad); code != 400 {
			t.Errorf("%s should be rejected, got %d", bad, code)
		}
	}
	if code, _ := e.do("PATCH", path, `{"date_of_birth":"1990-04-02","sex":"female","blood_group":"o+","height_cm":168.5}`); code != 200 {
		t.Fatalf("valid details rejected: %d", code)
	}
	_, env := e.do("GET", path, "")
	var got identity.ProfileResponse
	got = data[identity.ProfileResponse](t, env)
	if got.BloodGroup == nil || *got.BloodGroup != "O+" || got.Sex == nil || *got.Sex != "female" || got.HeightCM == nil || *got.HeightCM != 168.5 || got.DateOfBirth == nil || *got.DateOfBirth != "1990-04-02" {
		t.Fatalf("round trip: %+v", got)
	}
	// A partial update leaves the other details alone.
	e.do("PATCH", path, `{"height_cm":170}`)
	_, env = e.do("GET", path, "")
	if got = data[identity.ProfileResponse](t, env); got.Sex == nil || *got.Sex != "female" {
		t.Fatalf("partial update lost data: %+v", got)
	}
	_, env = e.do("GET", path+"/overview", "")
	o := data[commandcentre.Overview](t, env)
	if o.Profile.Age == nil || *o.Profile.Age < 30 {
		t.Fatalf("overview age: %+v", o.Profile)
	}
	for _, item := range o.Completeness.Items {
		if item.Key == "profile" && !item.Done {
			t.Error("profile item should be complete")
		}
	}
	if code, _ := e.doAs("intruder", "PATCH", path, `{"sex":"male"}`); code != 404 {
		t.Errorf("another owner must not edit: %d", code)
	}
}

func TestTimelineDateRange(t *testing.T) {
	e := commandCentre(t, "cc-range", storage()...)
	e.observe(t, "GLUCOSE", "Glucose", 95, "mg/dL", ago(10), "VERIFIED")
	e.do("POST", "/profiles/"+e.profile+"/history", fmt.Sprintf(`{"kind":"surgery","title":"Appendectomy","started_on":%q}`, ago(400)))
	base := "/profiles/" + e.profile + "/timeline"
	_, env := e.do("GET", fmt.Sprintf("%s?from=%s", base, ago(30)), "")
	if items := data[[]commandcentre.TimelineItem](t, env); len(items) != 1 || items[0].Kind != "observation" {
		t.Fatalf("from: %+v", items)
	}
	_, env = e.do("GET", fmt.Sprintf("%s?to=%s", base, ago(30)), "")
	if items := data[[]commandcentre.TimelineItem](t, env); len(items) != 1 || items[0].Kind != "history" {
		t.Fatalf("to: %+v", items)
	}
	_, env = e.do("GET", fmt.Sprintf("%s?from=%s&to=%s", base, ago(10), ago(10)), "")
	if items := data[[]commandcentre.TimelineItem](t, env); len(items) != 1 {
		t.Fatalf("a single day must include its own entries: %+v", items)
	}
	for _, q := range []string{"from=bad", fmt.Sprintf("from=%s&to=%s", ago(1), ago(5))} {
		if code, _ := e.do("GET", base+"?"+q, ""); code != 400 {
			t.Errorf("%s should be rejected: %d", q, code)
		}
	}
}

func TestSuggestedFollowUps(t *testing.T) {
	e := commandCentre(t, "cc-suggest", storage()...)
	for _, d := range []int{600, 500, 400} {
		e.observe(t, "HBA1C", "Hemoglobin A1c", 6.1, "%", ago(d), "VERIFIED")
	}
	e.do("POST", "/profiles/"+e.profile+"/goals", `{"biomarker_code":"HBA1C","unit":"%","target_max":5.7}`)
	_, env := e.do("GET", "/profiles/"+e.profile+"/overview", "")
	o := data[commandcentre.Overview](t, env)
	titles := map[string]bool{}
	for _, s := range o.Suggestions {
		titles[s.Title] = true
	}
	if !titles["Repeat Hemoglobin A1c"] {
		t.Fatalf("a measurement gap should be suggested: %+v", o.Suggestions)
	}
	if code, _ := e.do("POST", "/profiles/"+e.profile+"/follow-ups", `{"kind":"investigation","title":"Repeat Hemoglobin A1c"}`); code != 201 {
		t.Fatalf("create follow-up: %d", code)
	}
	_, env = e.do("GET", "/profiles/"+e.profile+"/overview", "")
	for _, s := range data[commandcentre.Overview](t, env).Suggestions {
		if s.Title == "Repeat Hemoglobin A1c" {
			t.Errorf("an open follow-up must suppress the suggestion: %+v", s)
		}
	}
}
