package agent

import (
	"context"
	"encoding/json"
	"net/http"
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
	if len(perms) != 4 {
		t.Fatalf("permissions: %v", out)
	}
	for _, p := range perms {
		m := p.(map[string]any)
		if (m["permission"] == "propose_actions") == m["granted"].(bool) {
			t.Fatalf("only propose_actions starts off: %v", m)
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
