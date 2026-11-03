package devices

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
		grant(t, c, profile)
	}
	return &fixture{t: t, c: c, h: testkit.Router("owner-a", New(c)), profile: profile}
}

func grant(t *testing.T, c *core.Core, profile string) {
	t.Helper()
	if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'health_data_integration',true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, profile); err != nil {
		t.Fatal(err)
	}
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

func data(out map[string]any) map[string]any { d, _ := out["data"].(map[string]any); return d }

func (f *fixture) device(body string) string {
	f.t.Helper()
	code, out := f.do("POST", "/profiles/"+f.profile+"/devices", body)
	if code != 201 && code != 200 {
		f.t.Fatalf("register device: %d %v", code, out)
	}
	return data(out)["id"].(string)
}

const watch = `{"kind":"smartwatch","source":"healthkit","name":"Apple Watch","manufacturer":"Apple","external_id":"hk-watch-1"}`

func at(daysAgo int, hour int) string {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -daysAgo).Add(time.Duration(hour) * time.Hour).Format(time.RFC3339)
}

func reading(metric string, value float64, unit, when string, id string) string {
	return fmt.Sprintf(`{"metric":%q,"value":%v,"unit":%q,"measured_at":%q,"external_id":%q}`, metric, value, unit, when, id)
}

func batch(readings ...string) string { return `{"readings":[` + strings.Join(readings, ",") + `]}` }

func TestRegisteringADeviceNeedsIntegrationConsent(t *testing.T) {
	f := newFixture(t, false)
	if code, out := f.do("POST", "/profiles/"+f.profile+"/devices", watch); code != 403 {
		t.Fatalf("without consent: %d %v", code, out)
	}
	grant(t, f.c, f.profile)
	id := f.device(watch)
	again := f.device(watch)
	if id != again {
		t.Fatalf("same external id must return the same device: %s vs %s", id, again)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM devices WHERE profile_id=$1`, f.profile); n != 1 {
		t.Fatalf("devices: %d", n)
	}
	for _, bad := range []string{
		`{"kind":"toaster","source":"healthkit","name":"x"}`,
		`{"kind":"smartwatch","source":"telepathy","name":"x"}`,
		`{"kind":"smartwatch","source":"healthkit","name":"  "}`,
		`{"kind":"smartwatch","source":"healthkit","name":"x","extra":1}`,
	} {
		if code, _ := f.do("POST", "/profiles/"+f.profile+"/devices", bad); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='device.connected'`, f.profile); n != 1 {
		t.Fatalf("connect must be audited once: %d", n)
	}
}

func TestSyncIsIdempotentAndReportsRejectedReadings(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	path := "/profiles/" + f.profile + "/devices/" + id + "/readings"
	body := batch(
		reading("heart_rate", 72, "bpm", at(0, 8), "a1"),
		reading("body_temperature", 98.6, "°F", at(0, 9), "a2"),
		reading("heart_rate", 900, "bpm", at(0, 10), "a3"),
		reading("heart_rate", 70, "mmHg", at(0, 11), "a4"),
		reading("mystery", 1, "x", at(0, 12), "a5"),
		reading("heart_rate", 70, "bpm", time.Now().UTC().Add(2*time.Hour).Format(time.RFC3339), "a6"),
		`{"metric":"heart_rate","unit":"bpm","measured_at":"`+at(0, 7)+`"}`,
	)
	code, out := f.do("POST", path, body)
	res := data(out)
	if code != 200 || res["accepted"].(float64) != 2 || res["rejected"].(float64) != 5 || len(res["errors"].([]any)) != 5 {
		t.Fatalf("first sync: %d %v", code, out)
	}
	code, out = f.do("POST", path, batch(reading("heart_rate", 72, "bpm", at(0, 8), "a1"), reading("heart_rate", 80, "bpm", at(0, 13), "a7")))
	if res = data(out); code != 200 || res["accepted"].(float64) != 1 || res["duplicates"].(float64) != 1 {
		t.Fatalf("re-sync: %d %v", code, out)
	}
	var stored float64
	var origUnit string
	if err := f.c.DB.Pool.QueryRow(context.Background(), `SELECT value,original_unit FROM device_readings WHERE device_id=$1 AND dedupe_key='a2'`, id).Scan(&stored, &origUnit); err != nil || stored < 36.99 || stored > 37.01 || origUnit != "°F" {
		t.Fatalf("temperature must be stored in °C with the original kept: %v %v %v", stored, origUnit, err)
	}

	_, out = f.do("GET", "/profiles/"+f.profile+"/devices/"+id+"/syncs", "")
	if syncs := out["data"].([]any); len(syncs) != 2 {
		t.Fatalf("sync history: %v", out)
	}
	_, out = f.do("GET", "/profiles/"+f.profile+"/devices", "")
	if d := out["data"].([]any)[0].(map[string]any); d["reading_count"].(float64) != 3 || d["last_synced_at"] == nil {
		t.Fatalf("device list: %v", d)
	}
}

func TestReadingsWithoutIDsAreDeduplicatedByContent(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	r := fmt.Sprintf(`{"metric":"heart_rate","value":71,"unit":"bpm","measured_at":%q}`, at(1, 8))
	path := "/profiles/" + f.profile + "/devices/" + id + "/readings"
	f.do("POST", path, batch(r))
	_, out := f.do("POST", path, batch(r, r))
	if res := data(out); res["accepted"].(float64) != 0 || res["duplicates"].(float64) != 2 {
		t.Fatalf("%v", out)
	}
}

func TestSyncGuards(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	path := "/profiles/" + f.profile + "/devices/" + id + "/readings"
	if code, _ := f.do("POST", path, `{"readings":[]}`); code != 400 {
		t.Fatalf("empty batch: %d", code)
	}
	var many []string
	for i := 0; i <= maxBatch; i++ {
		many = append(many, reading("steps", 1, "count", at(2, 1), fmt.Sprint("m", i)))
	}
	if code, _ := f.do("POST", path, batch(many...)); code != 400 {
		t.Fatalf("oversized batch: %d", code)
	}

	if code, _ := f.do("PATCH", "/profiles/"+f.profile+"/devices/"+id, `{"status":"disconnected"}`); code != 200 {
		t.Fatalf("disconnect: %d", code)
	}
	if code, out := f.do("POST", path, batch(reading("steps", 1, "count", at(1, 1), "x"))); code != 409 {
		t.Fatalf("disconnected sync: %d %v", code, out)
	}
	if code, _ := f.do("PATCH", "/profiles/"+f.profile+"/devices/"+id, `{"status":"connected","name":"Wrist"}`); code != 200 {
		t.Fatalf("reconnect: %d", code)
	}

	if _, err := f.c.DB.Pool.Exec(context.Background(), `UPDATE consents SET granted=false,revoked_at=now() WHERE profile_id=$1 AND purpose='health_data_integration'`, f.profile); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.do("POST", path, batch(reading("steps", 1, "count", at(1, 1), "y"))); code != 403 {
		t.Fatalf("revoked consent must stop syncing: %d", code)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action IN ('device.disconnected','device.reconnected')`, f.profile); n != 2 {
		t.Fatalf("status changes must be audited: %d", n)
	}
}

func TestOtherOwnersCannotSeeOrChangeDevices(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	base := "/profiles/" + f.profile
	for _, c := range []struct{ method, path, body string }{
		{"GET", base + "/devices", ""},
		{"POST", base + "/devices", watch},
		{"PATCH", base + "/devices/" + id, `{"name":"x"}`},
		{"DELETE", base + "/devices/" + id, ""},
		{"POST", base + "/devices/" + id + "/readings", batch(reading("steps", 1, "count", at(1, 1), "z"))},
		{"GET", base + "/device-readings", ""},
		{"GET", base + "/device-readings/summary", ""},
		{"GET", base + "/device-readings/steps/daily", ""},
	} {
		if code, _ := f.do(c.method, c.path, c.body, "owner-b"); code != 404 {
			t.Errorf("%s %s as another owner: %d", c.method, c.path, code)
		}
	}
	if code, _ := f.do("PATCH", base+"/devices/not-a-uuid", `{"name":"x"}`); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
}

func TestRemovingADeviceErasesItsReadings(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	f.do("POST", "/profiles/"+f.profile+"/devices/"+id+"/readings", batch(reading("heart_rate", 70, "bpm", at(1, 8), "r1"), reading("heart_rate", 71, "bpm", at(1, 9), "r2")))
	code, out := f.do("DELETE", "/profiles/"+f.profile+"/devices/"+id, "")
	if code != 200 || data(out)["readings_deleted"].(float64) != 2 {
		t.Fatalf("%d %v", code, out)
	}
	if n := testkit.Count(t, f.c, `SELECT (SELECT count(*) FROM device_readings WHERE profile_id=$1)+(SELECT count(*) FROM device_syncs WHERE profile_id=$1)`, f.profile); n != 0 {
		t.Fatalf("rows survived device removal: %d", n)
	}
	var details string
	if err := f.c.DB.Pool.QueryRow(context.Background(), `SELECT details::text FROM audit_events WHERE profile_id=$1 AND action='device.removed'`, f.profile).Scan(&details); err != nil || strings.Contains(details, "70") {
		t.Fatalf("audit must record counts only: %q %v", details, err)
	}
}

func TestSummaryDailySeriesAndListing(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	var rs []string
	for d := 0; d < 5; d++ {
		rs = append(rs, reading("resting_heart_rate", 64, "bpm", at(d, 8), fmt.Sprint("n", d)), reading("steps", 4000, "count", at(d, 10), fmt.Sprint("s", d)), reading("steps", 3000, "count", at(d, 16), fmt.Sprint("t", d)))
	}
	for d := 0; d < 5; d++ {
		rs = append(rs, reading("resting_heart_rate", 82, "bpm", at(40+d, 8), fmt.Sprint("o", d)))
	}
	f.do("POST", "/profiles/"+f.profile+"/devices/"+id+"/readings", batch(rs...))

	_, out := f.do("GET", "/profiles/"+f.profile+"/device-readings/summary", "")
	var rhr, steps map[string]any
	for _, s := range data(out)["summaries"].([]any) {
		switch s.(map[string]any)["metric"] {
		case "resting_heart_rate":
			rhr = s.(map[string]any)
		case "steps":
			steps = s.(map[string]any)
		}
	}
	if rhr == nil || rhr["trend"] != "falling" || rhr["outside_typical_range"] != false || rhr["latest_device"] != "Apple Watch" {
		t.Fatalf("resting heart rate summary: %v", rhr)
	}
	if steps == nil || steps["mean_7_days"].(float64) != 7000 {
		t.Fatalf("steps must average daily totals: %v", steps)
	}

	_, out = f.do("GET", "/profiles/"+f.profile+"/device-readings/steps/daily?days=10", "")
	days := data(out)["days"].([]any)
	if len(days) != 5 || days[0].(map[string]any)["sum"].(float64) != 7000 || days[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("daily: %v", out)
	}

	_, out = f.do("GET", "/profiles/"+f.profile+"/device-readings?metric=steps&limit=3", "")
	list := out["data"].([]any)
	if len(list) != 3 || list[0].(map[string]any)["measurement_kind"] != "device" {
		t.Fatalf("list: %v", out)
	}
	for _, bad := range []string{"?metric=nope", "?from=yesterday", "?limit=0"} {
		if code, _ := f.do("GET", "/profiles/"+f.profile+"/device-readings"+bad, ""); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	if code, _ := f.do("GET", "/profiles/"+f.profile+"/device-readings/steps/daily?days=999", ""); code != 400 {
		t.Fatalf("days bound: %d", code)
	}
}

func TestLabComparisonKeepsLabAndDeviceSeparate(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(`{"kind":"glucometer","source":"manual","name":"Home meter"}`)
	labDay := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.c.DB.Pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO biomarker_catalog(code,display_name,supported_units) VALUES('GLUCOSE','Glucose',ARRAY['mg/dL','mmol/L']) ON CONFLICT (code) DO NOTHING`)
	exec(`INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,source_type,verification_status) VALUES($1,'owner-a','GLUCOSE','Glucose',5.0,'mmol/L',$2,'LAB_DOCUMENT','VERIFIED')`, f.profile, labDay)
	exec(`INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,source_type,verification_status) VALUES($1,'owner-a','GLUCOSE','Glucose',140,'mg/dL',$2,'LAB_DOCUMENT','PENDING_REVIEW')`, f.profile, labDay)
	exec(`INSERT INTO observations(profile_id,owner_id,biomarker_code,name,value,unit,observed_at,source_type,verification_status) VALUES($1,'owner-a','GLUCOSE','Glucose',7,'weird',$2,'LAB_DOCUMENT','VERIFIED')`, f.profile, time.Now().UTC().AddDate(0, 0, -200).Format("2006-01-02"))
	f.do("POST", "/profiles/"+f.profile+"/devices/"+id+"/readings", batch(reading("blood_glucose", 99, "mg/dL", at(2, 8), "g1"), reading("blood_glucose", 111, "mg/dL", at(1, 8), "g2"), reading("blood_glucose", 300, "mg/dL", at(30, 8), "g3")))

	code, out := f.do("GET", "/profiles/"+f.profile+"/device-readings/blood_glucose/lab-comparison", "")
	d := data(out)
	pairs := d["pairs"].([]any)
	if code != 200 || len(pairs) != 1 {
		t.Fatalf("only verified, comparable labs form pairs: %d %v", code, out)
	}
	p := pairs[0].(map[string]any)
	lab, dev := p["lab"].(map[string]any), p["device"].(map[string]any)
	if lab["unit"] != "mg/dL" || lab["value"].(float64) != 90.1 || dev["reading_count"].(float64) != 2 || dev["mean"].(float64) != 105 || p["difference"].(float64) != 14.9 {
		t.Fatalf("pair: %v", p)
	}
	if ex := d["excluded"].([]any); len(ex) != 1 || !strings.Contains(ex[0].(map[string]any)["reason"].(string), "weird") {
		t.Fatalf("excluded: %v", d["excluded"])
	}
	if !strings.Contains(d["note"].(string), "not interchangeable") {
		t.Fatal("the distinction between lab and device must be stated")
	}
	if code, _ := f.do("GET", "/profiles/"+f.profile+"/device-readings/steps/lab-comparison", ""); code != 400 {
		t.Fatalf("metric without lab equivalent: %d", code)
	}
	if code, _ := f.do("GET", "/profiles/"+f.profile+"/device-readings/blood_glucose/lab-comparison?window_days=99", ""); code != 400 {
		t.Fatalf("window bound: %d", code)
	}
}

func TestLocalDateUsesTheDevicesUTCOffset(t *testing.T) {
	f := newFixture(t, true)
	id := f.device(watch)
	// 22:30 UTC is already the next calendar day at +05:30.
	when := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3).Add(22*time.Hour + 30*time.Minute)
	body := batch(fmt.Sprintf(`{"metric":"steps","value":500,"unit":"count","measured_at":%q,"external_id":"tz","utc_offset_minutes":330}`, when.Format(time.RFC3339)))
	f.do("POST", "/profiles/"+f.profile+"/devices/"+id+"/readings", body)
	var day string
	if err := f.c.DB.Pool.QueryRow(context.Background(), `SELECT local_date::text FROM device_readings WHERE device_id=$1`, id).Scan(&day); err != nil || day != when.AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("local date %q err %v", day, err)
	}
}

func TestMetricCatalogListsUnits(t *testing.T) {
	f := newFixture(t, false)
	code, out := f.do("GET", "/device-metrics", "")
	if code != 200 || len(out["data"].([]any)) < 10 {
		t.Fatalf("%d %v", code, out)
	}
}
