package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"
)

type fixture struct {
	t       *testing.T
	c       *core.Core
	profile string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	c := testkit.Open(t)
	return &fixture{t: t, c: c, profile: testkit.SeedProfile(t, c, "owner-a")}
}

func (f *fixture) do(method, path, body string, owner ...string) (int, map[string]any) {
	f.t.Helper()
	o := "owner-a"
	if len(owner) > 0 {
		o = owner[0]
	}
	w := testkit.Call(testkit.Router(o, New(f.c)), method, "/profiles/"+f.profile+path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func data(out map[string]any) map[string]any { d, _ := out["data"].(map[string]any); return d }

func (f *fixture) seedHighGlucose() {
	f.t.Helper()
	f.c.DB.Pool.Exec(context.Background(), `INSERT INTO biomarker_catalog(code,display_name,synonyms,supported_units) VALUES('GLUCOSE','Glucose','{}','{mmol/L}') ON CONFLICT DO NOTHING`)
	_, err := f.c.DB.Pool.Exec(context.Background(), `INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,reference_range,observed_at,source_type,verification_status)
		VALUES($1,'owner-a','GLUCOSE','Glucose',9.0,'mmol/L','3.9-5.6',$2,'LAB_DOCUMENT','VERIFIED')`, f.profile, time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"))
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestPermissionsDefaultAndPersist(t *testing.T) {
	f := newFixture(t)
	_, out := f.do("GET", "/agent/permissions", "")
	perms, _ := data(out)["permissions"].([]any)
	if len(perms) != 5 {
		t.Fatalf("permissions: %v", out)
	}
	for _, p := range perms {
		m := p.(map[string]any)
		off := m["permission"] == "propose_actions" || m["permission"] == "cloud_model"
		if off == m["granted"].(bool) {
			t.Fatalf("only propose_actions and cloud_model start off: %v", m)
		}
	}
	if c, _ := f.do("PUT", "/agent/permissions/propose_actions", `{"granted":true}`); c != 200 {
		t.Fatalf("set: %d", c)
	}
	if c, _ := f.do("PUT", "/agent/permissions/launch_missiles", `{"granted":true}`); c != 400 {
		t.Fatalf("unknown permission: %d", c)
	}
	if c, _ := f.do("PUT", "/agent/permissions/read_labs", `{"granted":false}`, "owner-b"); c != 404 {
		t.Fatalf("another owner must not edit permissions: %d", c)
	}
}

func TestPlanNeedsPermissionThenProposesOncePerResult(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	if c, _ := f.do("POST", "/agent/plan", "{}"); c != http.StatusForbidden {
		t.Fatalf("plan without permission: %d", c)
	}
	f.do("PUT", "/agent/permissions/propose_actions", `{"granted":true}`)
	_, out := f.do("POST", "/agent/plan", "{}")
	if data(out)["proposed"].(float64) != 1 {
		t.Fatalf("first plan: %v", out)
	}
	_, out = f.do("POST", "/agent/plan", "{}")
	if data(out)["proposed"].(float64) != 0 {
		t.Fatalf("a second run must not repeat itself: %v", out)
	}
	_, out = f.do("GET", "/agent/actions?status=proposed", "")
	acts, _ := data(out)["actions"].([]any)
	if len(acts) != 1 || !strings.Contains(acts[0].(map[string]any)["reason"].(string), "above") {
		t.Fatalf("actions: %v", out)
	}
}

func TestApproveCreatesAFollowUpAndRejectDoesNot(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	f.do("PUT", "/agent/permissions/propose_actions", `{"granted":true}`)
	f.do("POST", "/agent/plan", "{}")
	_, out := f.do("GET", "/agent/actions", "")
	id := data(out)["actions"].([]any)[0].(map[string]any)["id"].(string)

	if c, _ := f.do("POST", "/agent/actions/"+id+"/approve", "{}", "owner-b"); c != 404 {
		t.Fatalf("another owner must not approve: %d", c)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM follow_ups WHERE profile_id=$1`, f.profile); n != 0 {
		t.Fatalf("nothing may be created before approval, got %d", n)
	}
	if c, _ := f.do("POST", "/agent/actions/"+id+"/approve", "{}"); c != 200 {
		t.Fatalf("approve: %d", c)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM follow_ups WHERE profile_id=$1 AND status='open'`, f.profile); n != 1 {
		t.Fatalf("approval must create one follow-up, got %d", n)
	}
	if c, _ := f.do("POST", "/agent/actions/"+id+"/approve", "{}"); c != 404 {
		t.Fatalf("a decided action cannot be decided again: %d", c)
	}

	// A rejected suggestion creates nothing and is not suggested again.
	f.c.DB.Pool.Exec(context.Background(), `UPDATE agent_actions SET status='proposed',decided_at=NULL WHERE id=$1`, id)
	if c, _ := f.do("POST", "/agent/actions/"+id+"/reject", "{}"); c != 200 {
		t.Fatalf("reject: %d", c)
	}
	f.do("POST", "/agent/plan", "{}")
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM agent_actions WHERE profile_id=$1 AND status='proposed'`, f.profile); n != 0 {
		t.Fatalf("a rejected result must not return, got %d", n)
	}
	_, out = f.do("GET", "/agent/activity", "")
	if ev, _ := data(out)["events"].([]any); len(ev) < 2 {
		t.Fatalf("activity: %v", out)
	}
}

func TestBriefingAndVisitPrepRestateTheRecord(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	f.c.DB.Pool.Exec(context.Background(), `INSERT INTO symptom_entries(profile_id,owner_id,name,severity,started_at) VALUES($1,'owner-a','Headache',4,now())`, f.profile)
	f.c.DB.Pool.Exec(context.Background(), `INSERT INTO history_entries(profile_id,owner_id,kind,title) VALUES($1,'owner-a','medication','Metformin')`, f.profile)

	_, out := f.do("GET", "/agent/briefing", "")
	body, _ := json.Marshal(out)
	for _, want := range []string{"Glucose is above", "Headache"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("briefing missing %q: %s", want, body)
		}
	}

	_, out = f.do("GET", "/agent/visit-prep", "")
	body, _ = json.Marshal(out)
	if !strings.Contains(string(body), "Metformin") || !strings.Contains(string(body), "Glucose") {
		t.Fatalf("visit prep: %s", body)
	}
	if !strings.Contains(string(body), "What could explain my Glucose") || !strings.Contains(string(body), "When should I repeat") {
		t.Fatalf("visit prep questions: %s", body)
	}

	// Withdrawing read access removes the section instead of reading around the user's choice.
	f.do("PUT", "/agent/permissions/read_labs", `{"granted":false}`)
	_, out = f.do("GET", "/agent/briefing", "")
	body, _ = json.Marshal(out)
	if strings.Contains(string(body), "Glucose is above") {
		t.Fatalf("labs must be hidden without permission: %s", body)
	}
}

func TestSuggestionsComeFromTheRecord(t *testing.T) {
	f := newFixture(t)
	_, out := f.do("GET", "/agent/suggestions", "")
	if s, _ := data(out)["suggestions"].([]any); len(s) != 0 {
		t.Fatalf("empty record should give no suggestions: %v", out)
	}
	f.seedHighGlucose()
	_, out = f.do("GET", "/agent/suggestions", "")
	s, _ := data(out)["suggestions"].([]any)
	if len(s) == 0 || !strings.Contains(s[0].(string), "Glucose") {
		t.Fatalf("suggestions: %v", out)
	}
	if code, _ := f.do("GET", "/agent/suggestions", "", "owner-b"); code != 404 {
		t.Fatalf("other owner: %d", code)
	}
}

func TestChatAnswersFromTheRecordWithoutAModel(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	code, out := f.do("POST", "/agent/chat", `{"message":"What about my glucose?","mode":"explain"}`)
	d := data(out)
	if code != 200 || d["source"] != "records" {
		t.Fatalf("chat: %d %v", code, out)
	}
	if !strings.Contains(d["answer"].(string), "9") || len(d["steps"].([]any)) == 0 || len(d["actions"].([]any)) < 3 {
		t.Fatalf("answer should cite the stored value, show steps and offer actions: %v", d)
	}
}

func TestChatEmergencyNeverReachesAModel(t *testing.T) {
	f := newFixture(t)
	f.do("PUT", "/agent/permissions/cloud_model", `{"granted":true}`)
	t.Setenv("AI_SERVICE_URL", "http://127.0.0.1:1")
	_, out := f.do("POST", "/agent/chat", `{"message":"I have chest pain and my glucose is high"}`)
	if d := data(out); d["urgent"] != true || d["source"] != "safety" {
		t.Fatalf("emergency: %v", out)
	}
}

func TestChatUsesModelToolsOnlyWithCloudPermission(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	calls := 0
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Messages[len(in.Messages)-1]["role"] == "tool" {
			w.Write([]byte(`{"content":"Your Glucose is 9 mmol/L, above range.","tool_calls":[],"model":"fake"}`))
			return
		}
		w.Write([]byte(`{"content":"","tool_calls":[{"id":"c1","name":"get_trend","arguments":"{\"name\":\"Glucose\"}"}],"model":"fake"}`))
	}))
	defer ai.Close()
	t.Setenv("AI_SERVICE_URL", ai.URL)
	if _, out := f.do("POST", "/agent/chat", `{"message":"glucose?"}`); data(out)["source"] != "records" || calls != 0 {
		t.Fatalf("without the permission the model must not be called: %v calls=%d", out, calls)
	}
	f.do("PUT", "/agent/permissions/cloud_model", `{"granted":true}`)
	// One call is the Copilot attempt, which this HF-shaped fake cannot answer; two are the HF tool loop.
	_, out := f.do("POST", "/agent/chat", `{"message":"glucose?"}`)
	d := data(out)
	if d["source"] != "model" || d["model"] != "fake" || calls != 3 || len(d["steps"].([]any)) != 1 {
		t.Fatalf("model loop: %v calls=%d", out, calls)
	}
}

func TestChatRejectsUngroundedModelAnswer(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	f.do("PUT", "/agent/permissions/cloud_model", `{"granted":true}`)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content":"Your glucose is 42 and you have diabetes.","tool_calls":[],"model":"fake"}`))
	}))
	defer ai.Close()
	t.Setenv("AI_SERVICE_URL", ai.URL)
	_, out := f.do("POST", "/agent/chat", `{"message":"glucose?"}`)
	if d := data(out); d["source"] != "records" || strings.Contains(d["answer"].(string), "diabetes") {
		t.Fatalf("an answer with invented numbers or a diagnosis must be replaced: %v", out)
	}
}

func TestChatNeedsLabPermissionAndOwnership(t *testing.T) {
	f := newFixture(t)
	if c, _ := f.do("POST", "/agent/chat", `{"message":"hi"}`, "owner-b"); c != 404 {
		t.Fatalf("other owner: %d", c)
	}
	f.do("PUT", "/agent/permissions/read_labs", `{"granted":false}`)
	if c, _ := f.do("POST", "/agent/chat", `{"message":"hi"}`); c != 403 {
		t.Fatalf("no lab permission: %d", c)
	}
}

func fakeCopilot(t *testing.T, answer string, tools ...string) {
	t.Helper()
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/copilot" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Message  string         `json:"message"`
			Snapshot map[string]any `json:"snapshot"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Snapshot["get_care_plan"] == nil || in.Snapshot["list_medications"] == nil {
			t.Errorf("snapshot is missing tools: %v", in.Snapshot)
		}
		calls := []map[string]any{}
		for _, tool := range tools {
			calls = append(calls, map[string]any{"tool": tool, "args": map[string]any{}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": answer, "calls": calls, "model": "copilot-fake"})
	}))
	t.Cleanup(ai.Close)
	t.Setenv("AI_SERVICE_URL", ai.URL)
}

func TestChatPrefersCopilotAndStaysGrounded(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	fakeCopilot(t, "Your Glucose is 9 mmol/L, above range.", "list_flagged_labs")
	f.do("PUT", "/agent/permissions/cloud_model", `{"granted":true}`)
	_, out := f.do("POST", "/agent/chat", `{"message":"what is high?"}`)
	if d := data(out); d["source"] != "copilot" || d["model"] != "copilot-fake" || len(d["steps"].([]any)) != 1 {
		t.Fatalf("copilot chat: %v", out)
	}
	fakeCopilot(t, "Your Glucose is 99 mmol/L.", "list_flagged_labs")
	_, out = f.do("POST", "/agent/chat", `{"message":"what is high?"}`)
	if d := data(out); d["source"] != "records" {
		t.Fatalf("an invented number must fall back to the record: %v", out)
	}
}

func TestBriefingSummaryNeedsCloudPermissionAndListsMedicines(t *testing.T) {
	f := newFixture(t)
	f.seedHighGlucose()
	fakeCopilot(t, "Glucose is above range at 9 mmol/L.", "list_flagged_labs", "get_care_plan")
	if _, out := f.do("GET", "/agent/briefing/summary", ""); data(out)["source"] != "none" {
		t.Fatalf("no cloud permission must give no summary: %v", out)
	}
	f.do("PUT", "/agent/permissions/cloud_model", `{"granted":true}`)
	_, out := f.do("GET", "/agent/briefing/summary", "")
	if d := data(out); d["source"] != "copilot" || len(d["steps"].([]any)) != 2 {
		t.Fatalf("summary: %v", out)
	}
	if _, err := f.c.DB.Pool.Exec(context.Background(), `WITH rx AS (INSERT INTO prescriptions(profile_id,owner_id) VALUES($1,'owner-a') RETURNING id)
		INSERT INTO prescription_items(prescription_id,profile_id,name,strength,slots) SELECT id,$1,'Metformin','500 mg',ARRAY['morning','night'] FROM rx`, f.profile); err != nil {
		t.Fatal(err)
	}
	_, out = f.do("GET", "/agent/briefing?day=2026-01-02", "")
	found := false
	for _, s := range data(out)["sections"].([]any) {
		sec := s.(map[string]any)
		if sec["key"] == "medicines" && len(sec["items"].([]any)) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("briefing should list the morning and night doses: %v", out)
	}
}

func TestVisitPrepListsSavedPrescriptionsOnceAlongsideHistory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.c.DB.Pool.Exec(ctx, `WITH rx AS (INSERT INTO prescriptions(profile_id,owner_id) VALUES($1,'owner-a') RETURNING id)
		INSERT INTO prescription_items(prescription_id,profile_id,name,strength,frequency_text) SELECT id,$1,'Metformin','500 mg','twice daily' FROM rx`, f.profile); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Metformin", "Vitamin D"} {
		if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO history_entries(profile_id,owner_id,kind,title,status) VALUES($1,'owner-a','medication',$2,'active')`, f.profile, title); err != nil {
			t.Fatal(err)
		}
	}
	_, out := f.do("GET", "/agent/visit-prep", "")
	meds := data(out)["medications"].([]any)
	if len(meds) != 2 || !strings.Contains(meds[0].(string), "twice daily") || meds[1] != "Vitamin D" {
		t.Fatalf("expected the prescription once plus Vitamin D: %v", meds)
	}
}

func TestWellbeingPlanUsesOwnDataAndStaysEmptyWithoutIt(t *testing.T) {
	f := newFixture(t)
	_, out := f.do("GET", "/agent/wellbeing?day=2026-11-05", "")
	d := data(out)
	if tasks, _ := d["tasks"].([]any); len(tasks) != 0 || d["score"].(float64) != 0 {
		t.Fatalf("empty profile must not invent tasks or a score: %v", d)
	}

	f.seedHighGlucose()
	f.c.DB.Pool.Exec(context.Background(), `INSERT INTO follow_ups(profile_id,owner_id,kind,title,due_on) VALUES($1,'owner-a','investigation','Repeat HbA1c','2026-11-01')`, f.profile)
	// A fortnight of good sleep, then a poor week: the plan should coach sleep against the person's own baseline.
	for i := 0; i < 30; i++ {
		v := 7.5
		if i < 7 {
			v = 5.5
		}
		f.c.DB.Pool.Exec(context.Background(), `INSERT INTO lifestyle_logs(profile_id,owner_id,category,value,unit,logged_on) VALUES($1,'owner-a','sleep',$2,'h','2026-11-05'::date-$3::int)`, f.profile, v, i)
	}
	_, out = f.do("GET", "/agent/wellbeing?day=2026-11-05", "")
	body, _ := json.Marshal(out)
	for _, want := range []string{"Overdue by 4 days", "Review your Glucose", "Bring your sleep back toward", "slipping"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("plan missing %q: %s", want, body)
		}
	}
	if s := data(out)["score"].(float64); s <= 0 || s > 100 {
		t.Fatalf("score %v", s)
	}
}

func TestLabFallbackFlagsOutOfRangeAndCountsTheRest(t *testing.T) {
	prev := 4.8
	got := labFallback([]labLine{
		{Name: "Glucose", Value: 9, Unit: "mmol/L", Flag: "above_range", Range: "3.9-5.6", Previous: &prev},
		{Name: "TSH", Value: 2, Unit: "mIU/L", Flag: "normal"},
	})
	if got.Headline != "1 of 2 results are in range." || len(got.Items) != 2 {
		t.Fatalf("unexpected fallback: %+v", got)
	}
	if got.Items[0].Tone != "act" || !strings.Contains(got.Items[0].Detail, "previously 4.8") || got.Items[1].Tone != "good" {
		t.Fatalf("items: %+v", got.Items)
	}
}
