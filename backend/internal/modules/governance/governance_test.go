package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

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

func (f *fixture) do(owner, method, path, body string) (int, map[string]any) {
	f.t.Helper()
	w := testkit.Call(testkit.Router(owner, New(f.c)), method, path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if data, ok := out["data"].(map[string]any); ok {
		out = data
	}
	return w.Code, out
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.c.DB.Pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}

func TestRetentionPolicyValidationAndPurge(t *testing.T) {
	f := newFixture(t)
	base := "/profiles/" + f.profile + "/retention"
	if code, out := f.do("owner-a", "GET", base, ""); code != 200 || out["ai_history_days"] != nil {
		t.Fatalf("default policy: %d %v", code, out)
	}
	for _, body := range []string{`{"ai_history_days":0}`, `{"device_readings_days":4000}`, `{"ai_history_days":"x"}`} {
		if code, _ := f.do("owner-a", "PUT", base, body); code != 400 {
			t.Fatalf("%s: %d", body, code)
		}
	}
	if code, _ := f.do("owner-b", "PUT", base, `{"ai_history_days":30}`); code != 404 {
		t.Fatalf("other owner: %d", code)
	}
	f.exec(`INSERT INTO health_questions(profile_id,owner_id,question,answer,created_at) VALUES($1,'owner-a','old','a',now()-interval '40 days'),($1,'owner-a','new','a',now())`, f.profile)
	if code, out := f.do("owner-a", "PUT", base, `{"ai_history_days":30}`); code != 200 || out["ai_history_days"] != float64(30) {
		t.Fatalf("set: %d %v", code, out)
	}
	if code, out := f.do("owner-a", "POST", base+"/apply", ""); code != 200 || out["ai_history_removed"].(float64) != 1 || out["device_readings_removed"].(float64) != 0 {
		t.Fatalf("apply: %d %v", code, out)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM health_questions WHERE profile_id=$1`, f.profile); n != 1 {
		t.Fatalf("remaining answers %d", n)
	}
	if code, out := f.do("owner-a", "GET", base, ""); code != 200 || out["last_applied_at"] == nil {
		t.Fatalf("applied time: %v", out)
	}
	if code, _ := f.do("owner-a", "PUT", base, `{}`); code != 200 {
		t.Fatalf("clear: %d", code)
	}
	if code, _ := f.do("owner-b", "POST", base+"/apply", ""); code != 404 {
		t.Fatalf("other owner apply: %d", code)
	}
}

func TestSharingLifecycle(t *testing.T) {
	f := newFixture(t)
	f.exec(`UPDATE profiles SET display_name='Asha' WHERE id=$1`, f.profile)
	f.exec(`INSERT INTO observations(profile_id,owner_id,name,value,unit) VALUES($1,'owner-a','Hemoglobin',13.5,'g/dL')`, f.profile)
	f.exec(`INSERT INTO history_entries(owner_id,profile_id,kind,title) VALUES('owner-a',$1,'allergy','Penicillin')`, f.profile)
	base := "/profiles/" + f.profile + "/shares"

	for _, body := range []string{`{}`, `{"label":"Mum","scopes":["everything"]}`, `{"label":"Mum","scopes":["labs"],"expires_in_days":400}`, `{"label":"","scopes":["labs"]}`} {
		if code, _ := f.do("owner-a", "POST", base, body); code != 400 {
			t.Fatalf("%s: %d", body, code)
		}
	}
	if code, _ := f.do("owner-b", "POST", base, `{"label":"Mum","scopes":["labs"]}`); code != 404 {
		t.Fatalf("other owner create: %d", code)
	}
	code, created := f.do("owner-a", "POST", base, `{"label":"Mum","scopes":["labs","summary","labs"]}`)
	if code != 201 {
		t.Fatalf("create: %d %v", code, created)
	}
	share := created["share"].(map[string]any)
	id, secret := share["id"].(string), created["code"].(string)
	if share["status"] != "pending" || len(share["scopes"].([]any)) != 2 {
		t.Fatalf("share: %v", share)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM profile_shares WHERE code_hash=$1`, secret); n != 0 {
		t.Fatal("the invite code must be stored hashed")
	}

	if code, _ := f.do("owner-b", "GET", "/shared-with-me/"+id, ""); code != 404 {
		t.Fatalf("unaccepted view: %d", code)
	}
	if code, _ := f.do("owner-b", "POST", "/shares/accept", `{"code":"nope"}`); code != 404 {
		t.Fatalf("bad code: %d", code)
	}
	if code, _ := f.do("owner-b", "POST", "/shares/accept", `{}`); code != 400 {
		t.Fatalf("empty code: %d", code)
	}
	if code, _ := f.do("owner-a", "POST", "/shares/accept", `{"code":"`+secret+`"}`); code != 400 {
		t.Fatalf("own share: %d", code)
	}
	if code, _ := f.do("owner-b", "POST", "/shares/accept", `{"code":"`+secret+`"}`); code != 200 {
		t.Fatalf("accept: %d", code)
	}
	if code, _ := f.do("owner-c", "POST", "/shares/accept", `{"code":"`+secret+`"}`); code != 404 {
		t.Fatalf("code reuse: %d", code)
	}

	if code, out := f.do("owner-b", "GET", "/shared-with-me", ""); code != 200 || len(out["shares"].([]any)) != 1 {
		t.Fatalf("shared list: %d %v", code, out)
	}
	code, view := f.do("owner-b", "GET", "/shared-with-me/"+id, "")
	if code != 200 || view["read_only"] != true || view["display_name"] != "Asha" || view["labs"] == nil || view["summary"] == nil {
		t.Fatalf("view: %d %v", code, view)
	}
	if _, leaked := view["history"]; leaked {
		t.Fatal("history was not in scope")
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='share.viewed'`, f.profile); n != 1 {
		t.Fatalf("view audit %d", n)
	}
	if code, _ := f.do("owner-c", "GET", "/shared-with-me/"+id, ""); code != 404 {
		t.Fatalf("stranger view: %d", code)
	}
	if code, out := f.do("owner-a", "GET", base, ""); code != 200 || out["shares"].([]any)[0].(map[string]any)["status"] != "active" {
		t.Fatalf("owner list: %v", out)
	}

	if code, _ := f.do("owner-b", "DELETE", base+"/"+id, ""); code != 404 {
		t.Fatalf("grantee revoke: %d", code)
	}
	if code, _ := f.do("owner-a", "DELETE", base+"/not-a-uuid", ""); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
	if code, _ := f.do("owner-a", "DELETE", base+"/"+id, ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := f.do("owner-b", "GET", "/shared-with-me/"+id, ""); code != 404 {
		t.Fatalf("revoked view: %d", code)
	}
}

func TestSharingExpiryLimitAndLeave(t *testing.T) {
	f := newFixture(t)
	base := "/profiles/" + f.profile + "/shares"
	_, created := f.do("owner-a", "POST", base, `{"label":"Doctor","scopes":["follow_ups","history"],"expires_in_days":1}`)
	id, secret := created["share"].(map[string]any)["id"].(string), created["code"].(string)
	if code, _ := f.do("owner-b", "POST", "/shares/accept", `{"code":"`+secret+`"}`); code != 200 {
		t.Fatal("accept")
	}
	if code, view := f.do("owner-b", "GET", "/shared-with-me/"+id, ""); code != 200 || view["history"] == nil || view["follow_ups"] == nil {
		t.Fatalf("scoped view: %v", view)
	}
	if code, _ := f.do("owner-b", "DELETE", "/shared-with-me/"+id, ""); code != 204 {
		t.Fatalf("leave: %d", code)
	}
	if code, _ := f.do("owner-b", "DELETE", "/shared-with-me/"+id, ""); code != 404 {
		t.Fatalf("leave twice: %d", code)
	}
	if code, _ := f.do("owner-b", "DELETE", "/shared-with-me/bad", ""); code != 404 {
		t.Fatalf("leave bad id: %d", code)
	}
	if code, _ := f.do("owner-b", "GET", "/shared-with-me/bad", ""); code != 404 {
		t.Fatalf("view bad id: %d", code)
	}

	_, expiring := f.do("owner-a", "POST", base, `{"label":"Old","scopes":["labs"]}`)
	f.exec(`UPDATE profile_shares SET expires_at=now()-interval '1 day' WHERE id=$1`, expiring["share"].(map[string]any)["id"])
	if code, _ := f.do("owner-b", "POST", "/shares/accept", `{"code":"`+expiring["code"].(string)+`"}`); code != 404 {
		t.Fatalf("expired accept: %d", code)
	}

	for i := 0; i < maxActiveShares; i++ {
		f.do("owner-a", "POST", base, `{"label":"x","scopes":["labs"]}`)
	}
	if code, _ := f.do("owner-a", "POST", base, `{"label":"x","scopes":["labs"]}`); code != http.StatusConflict {
		t.Fatalf("limit: %d", code)
	}
}

func TestExportIncludesNewGovernanceData(t *testing.T) {
	f := newFixture(t)
	f.do("owner-a", "PUT", "/profiles/"+f.profile+"/retention", `{"ai_history_days":90}`)
	f.do("owner-a", "POST", "/profiles/"+f.profile+"/shares", `{"label":"Mum","scopes":["labs"]}`)
	code, out := f.do("owner-a", "GET", "/profiles/"+f.profile+"/export", "")
	if code != 200 || out["retention_policy"] == nil || len(out["shares"].([]any)) != 1 {
		t.Fatalf("export: %d %v", code, out)
	}
	if _, ok := out["shares"].([]any)[0].(map[string]any)["code_hash"]; ok {
		t.Fatal("export must not contain code hashes")
	}
}
