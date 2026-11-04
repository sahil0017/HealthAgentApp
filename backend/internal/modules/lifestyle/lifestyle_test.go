package lifestyle

import (
	"context"
	"encoding/json"
	"fmt"
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
	h       http.Handler
	profile string
}

func newFixture(t *testing.T, consent bool) *fixture {
	t.Helper()
	c := testkit.Open(t)
	profile := testkit.SeedProfile(t, c, "owner-a")
	if consent {
		if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'health_data',true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, profile); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{t: t, c: c, h: testkit.Router("owner-a", New(c)), profile: profile}
}

func (f *fixture) do(method, path, body string, owner ...string) (int, map[string]any) {
	f.t.Helper()
	h := f.h
	if len(owner) > 0 {
		h = testkit.Router(owner[0], New(f.c))
	}
	w := testkit.Call(h, method, path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (f *fixture) url(suffix string) string { return "/profiles/" + f.profile + suffix }

func day(ago int) string { return time.Now().UTC().AddDate(0, 0, -ago).Format("2006-01-02") }

func at(ago int) string {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -ago).Add(10 * time.Hour).Format(time.RFC3339)
}

func data(out map[string]any) map[string]any { d, _ := out["data"].(map[string]any); return d }

func list(out map[string]any) []any { l, _ := out["data"].([]any); return l }

func TestWritesNeedStorageConsentAndOwnership(t *testing.T) {
	f := newFixture(t, false)
	for path, body := range map[string]string{"/symptoms": `{"name":"Headache","severity":4}`, "/lifestyle-logs": `{"category":"sleep","value":7}`} {
		if code, _ := f.do("POST", f.url(path), body); code != 403 {
			t.Errorf("%s without consent: %d", path, code)
		}
	}
	if code, _ := f.do("GET", f.url("/symptoms"), "", "someone-else"); code != 404 {
		t.Fatalf("other owner: %d", code)
	}
	if code, _ := f.do("GET", f.url("/lifestyle/summary"), "", "someone-else"); code != 404 {
		t.Fatalf("other owner summary: %d", code)
	}
}

func TestSymptomLifecycle(t *testing.T) {
	f := newFixture(t, true)
	code, out := f.do("POST", f.url("/symptoms"), fmt.Sprintf(`{"name":" Headache ","severity":6,"started_at":%q,"notes":"after work"}`, at(2)))
	if code != 201 {
		t.Fatalf("create: %d %v", code, out)
	}
	id := data(out)["id"].(string)
	if d := data(out); d["name"] != "Headache" || d["active"] != true || d["severity"].(float64) != 6 {
		t.Fatalf("created: %v", d)
	}
	for _, bad := range []string{
		`{"name":"","severity":4}`, `{"severity":4}`, `{"name":"x"}`, `{"name":"x","severity":0}`, `{"name":"x","severity":11}`,
		`{"name":"x","severity":3,"extra":1}`, `{"name":"x","severity":3,"started_at":"2999-01-01T00:00:00Z"}`,
		fmt.Sprintf(`{"name":"x","severity":3,"started_at":%q,"ended_at":%q}`, at(1), at(2)),
		fmt.Sprintf(`{"name":"x","severity":3,"notes":%q}`, strings.Repeat("n", 1001)),
	} {
		if c, _ := f.do("POST", f.url("/symptoms"), bad); c != 400 {
			t.Errorf("%.40s: %d", bad, c)
		}
	}
	if c, _ := f.do("POST", f.url("/symptoms"), `{"name":"Nausea","severity":2}`); c != 201 {
		t.Fatalf("default start: %d", c)
	}
	_, out = f.do("GET", f.url("/symptoms?name=headache&active=true"), "")
	if len(list(out)) != 1 {
		t.Fatalf("filter: %v", out)
	}
	_, out = f.do("GET", f.url("/symptoms?from="+day(1)), "")
	if len(list(out)) != 1 {
		t.Fatalf("from filter: %v", out)
	}
	for _, q := range []string{"?active=maybe", "?from=bad", "?limit=0"} {
		if c, _ := f.do("GET", f.url("/symptoms"+q), ""); c != 400 {
			t.Errorf("%s: %d", q, c)
		}
	}
	if c, _ := f.do("PATCH", f.url("/symptoms/"+id), `{}`); c != 400 {
		t.Fatalf("empty patch: %d", c)
	}
	if c, _ := f.do("PATCH", f.url("/symptoms/"+id), `{"severity":12}`); c != 400 {
		t.Fatalf("bad severity: %d", c)
	}
	if c, _ := f.do("PATCH", f.url("/symptoms/"+id), fmt.Sprintf(`{"ended_at":%q}`, at(5))); c != 400 {
		t.Fatalf("end before start: %d", c)
	}
	if c, _ := f.do("PATCH", f.url("/symptoms/"+id), `{"notes":"`+strings.Repeat("n", 1001)+`"}`); c != 400 {
		t.Fatalf("long notes: %d", c)
	}
	code, out = f.do("PATCH", f.url("/symptoms/"+id), fmt.Sprintf(`{"severity":3,"ended_at":%q,"notes":""}`, at(1)))
	if d := data(out); code != 200 || d["active"] != false || d["severity"].(float64) != 3 || d["notes"] != nil {
		t.Fatalf("resolve: %d %v", code, out)
	}
	_, out = f.do("GET", f.url("/symptoms/summary"), "")
	groups := data(out)["symptoms"].([]any)
	if len(groups) != 2 || data(out)["note"] == "" {
		t.Fatalf("summary: %v", out)
	}
	if c, _ := f.do("GET", f.url("/symptoms/summary?days=0"), ""); c != 400 {
		t.Fatalf("window: %d", c)
	}
	if c, _ := f.do("PATCH", f.url("/symptoms/"+id), `{"severity":5}`, "someone-else"); c != 404 {
		t.Fatalf("other owner patch: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/symptoms/"+id), "", "someone-else"); c != 404 {
		t.Fatalf("other owner delete: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/symptoms/not-a-uuid"), ""); c != 404 {
		t.Fatalf("bad id: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/symptoms/"+id), ""); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/symptoms/"+id), ""); c != 404 {
		t.Fatalf("delete again: %d", c)
	}
}

func TestLifestyleLogsValidateAndSummarise(t *testing.T) {
	f := newFixture(t, true)
	if c, out := f.do("GET", "/lifestyle-categories", ""); c != 200 || len(list(out)) != 6 {
		t.Fatalf("categories: %d %v", c, out)
	}
	for _, bad := range []string{
		`{"category":"sleep"}`, `{"category":"yoga","value":1}`, `{"category":"sleep","value":25}`, `{"category":"mood","value":0}`,
		`{"category":"sleep","value":7,"logged_on":"nope"}`, `{"category":"sleep","value":7,"logged_on":"2999-01-01"}`,
		`{"category":"sleep","value":7,"label":"` + strings.Repeat("n", 121) + `"}`, `{"category":"sleep","value":7,"junk":1}`,
	} {
		if c, _ := f.do("POST", f.url("/lifestyle-logs"), bad); c != 400 {
			t.Errorf("%.50s: %d", bad, c)
		}
	}
	post := func(body string) string {
		t.Helper()
		c, out := f.do("POST", f.url("/lifestyle-logs"), body)
		if c != 201 {
			t.Fatalf("%s: %d %v", body, c, out)
		}
		return data(out)["id"].(string)
	}
	id := post(fmt.Sprintf(`{"category":"exercise","value":30,"label":"Run","logged_on":%q}`, day(1)))
	post(fmt.Sprintf(`{"category":"exercise","value":15,"logged_on":%q}`, day(1)))
	post(fmt.Sprintf(`{"category":"mood","value":2,"logged_on":%q}`, day(1)))
	post(fmt.Sprintf(`{"category":"mood","value":4,"logged_on":%q}`, day(1)))
	post(`{"category":"sleep","value":7.5,"notes":"ok"}`)
	_, out := f.do("GET", f.url("/lifestyle-logs?category=exercise"), "")
	if len(list(out)) != 2 {
		t.Fatalf("by category: %v", out)
	}
	_, out = f.do("GET", f.url("/lifestyle-logs?from="+day(0)+"&to="+day(0)), "")
	if len(list(out)) != 1 {
		t.Fatalf("by date: %v", out)
	}
	for _, q := range []string{"?category=zzz", "?from=x", "?offset=-1"} {
		if c, _ := f.do("GET", f.url("/lifestyle-logs"+q), ""); c != 400 {
			t.Errorf("%s: %d", q, c)
		}
	}
	_, out = f.do("GET", f.url("/lifestyle/summary?days=7"), "")
	byCat := map[string]map[string]any{}
	for _, c := range data(out)["categories"].([]any) {
		m := c.(map[string]any)
		byCat[m["category"].(string)] = m
	}
	if byCat["exercise"]["average_per_day"].(float64) != 45 || byCat["mood"]["average_per_day"].(float64) != 3 || byCat["sleep"]["days_logged"].(float64) != 1 || byCat["hydration"]["latest"] != nil {
		t.Fatalf("summary: %v", byCat)
	}
	if c, _ := f.do("GET", f.url("/lifestyle/summary?days=999"), ""); c != 400 {
		t.Fatalf("window: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/lifestyle-logs/"+id), "", "someone-else"); c != 404 {
		t.Fatalf("other owner: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/lifestyle-logs/nope"), ""); c != 404 {
		t.Fatalf("bad id: %d", c)
	}
	if c, _ := f.do("DELETE", f.url("/lifestyle-logs/"+id), ""); c != 204 {
		t.Fatalf("delete: %d", c)
	}
}

func TestCorrelationsFindAssociationsAndAdmitInsufficientData(t *testing.T) {
	f := newFixture(t, true)
	_, out := f.do("GET", f.url("/lifestyle/correlations"), "")
	for _, c := range data(out)["correlations"].([]any) {
		if c.(map[string]any)["status"] != "insufficient_data" {
			t.Fatalf("empty must be insufficient: %v", c)
		}
	}
	// Less sleep on a day means a worse headache the same day, and a worse one the next day.
	for ago := 1; ago <= 10; ago++ {
		hours := 4 + ago%5
		f.do("POST", f.url("/lifestyle-logs"), fmt.Sprintf(`{"category":"sleep","value":%d,"logged_on":%q}`, hours, day(ago)))
		f.do("POST", f.url("/symptoms"), fmt.Sprintf(`{"name":"Headache","severity":%d,"started_at":%q}`, 10-hours+1, at(ago)))
	}
	f.do("POST", f.url("/symptoms"), fmt.Sprintf(`{"name":"Cough","severity":9,"started_at":%q}`, at(3)))
	read := func(q string) map[string]any {
		c, out := f.do("GET", f.url("/lifestyle/correlations"+q), "")
		if c != 200 {
			t.Fatalf("%s: %d %v", q, c, out)
		}
		for _, row := range data(out)["correlations"].([]any) {
			if m := row.(map[string]any); m["category"] == "sleep" {
				return m
			}
		}
		t.Fatal("no sleep row")
		return nil
	}
	same := read("?symptom=headache&days=30")
	if same["status"] != "ok" || same["r"].(float64) != -1 || same["strength"] != "strong negative" || same["description"] == "" {
		t.Fatalf("same day: %v", same)
	}
	if next := read("?symptom=headache&days=30&lag=1"); next["status"] != "ok" || next["days"].(float64) < 7 {
		t.Fatalf("lag: %v", next)
	}
	if any := read("?days=30"); any["status"] != "ok" {
		t.Fatalf("any symptom: %v", any)
	}
	for _, q := range []string{"?lag=2", "?days=0", "?symptom=" + strings.Repeat("n", 121) + "x"} {
		if c, _ := f.do("GET", f.url("/lifestyle/correlations"+q), ""); c != 400 {
			t.Errorf("%.30s: %d", q, c)
		}
	}
	if c, _ := f.do("GET", f.url("/lifestyle/correlations"), "", "someone-else"); c != 404 {
		t.Fatalf("other owner: %d", c)
	}
}

func TestProfileDeletionRemovesLifestyleData(t *testing.T) {
	f := newFixture(t, true)
	f.do("POST", f.url("/symptoms"), `{"name":"Cough","severity":2}`)
	f.do("POST", f.url("/lifestyle-logs"), `{"category":"sleep","value":8}`)
	if _, err := f.c.DB.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, f.profile); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, f.c, `SELECT (SELECT count(*) FROM symptom_entries)+(SELECT count(*) FROM lifestyle_logs)`); n != 0 {
		t.Fatalf("orphans: %d", n)
	}
}
