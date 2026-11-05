package prescriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"
)

func setup(t *testing.T, purposes ...string) (*core.Core, string) {
	t.Helper()
	c := testkit.Open(t)
	profile := testkit.SeedProfile(t, c, "owner-a")
	for _, p := range purposes {
		if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,$2,true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, profile, p); err != nil {
			t.Fatal(err)
		}
	}
	return c, profile
}

func call(h http.Handler, method, path, body string) (int, map[string]any) {
	w := testkit.Call(h, method, path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestSaveListScheduleDelete(t *testing.T) {
	c, p := setup(t, "health_data")
	h := testkit.Router("owner-a", New(c))
	base := "/profiles/" + p + "/prescriptions"
	body := `{"doctor":"Dr A","source":"photo","items":[
		{"name":"Metformin","strength":"500 mg","frequency_text":"1-0-1","duration_text":"30 days","slots":["night","morning"],"with_food":"after"},
		{"name":"Paracetamol","as_needed":true},
		{"name":"Mystery"}]}`
	if code, _ := call(h, "POST", base, body); code != http.StatusCreated {
		t.Fatalf("save = %d", code)
	}
	if code, _ := call(h, "POST", base, `{"items":[{"name":"X","slots":["lunchtime"]}]}`); code != http.StatusBadRequest {
		t.Fatalf("bad slot = %d", code)
	}
	if code, out := call(h, "GET", base+"/schedule", ""); code != 200 {
		t.Fatalf("schedule = %d", code)
	} else {
		data := out["data"].(map[string]any)
		slots := data["slots"].([]any)
		if len(slots[0].(map[string]any)["items"].([]any)) != 1 || len(slots[3].(map[string]any)["items"].([]any)) != 1 {
			t.Fatalf("morning/night not populated: %v", slots)
		}
		if len(data["as_needed"].([]any)) != 1 || len(data["unscheduled"].([]any)) != 1 {
			t.Fatalf("as-needed/unscheduled wrong: %v", data)
		}
	}
	_, out := call(h, "GET", base, "")
	list := out["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("list = %v", out)
	}
	rx := list[0].(map[string]any)["id"].(string)
	if code, _ := call(testkit.Router("owner-b", New(c)), "DELETE", base+"/"+rx, ""); code != http.StatusNotFound {
		t.Fatalf("other owner delete = %d", code)
	}
	if code, _ := call(h, "DELETE", base+"/"+rx, ""); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
}

func TestSaveNeedsConsent(t *testing.T) {
	c, p := setup(t)
	h := testkit.Router("owner-a", New(c))
	if code, _ := call(h, "POST", "/profiles/"+p+"/prescriptions", `{"items":[{"name":"X"}]}`); code != http.StatusForbidden {
		t.Fatalf("code = %d", code)
	}
}

func TestReadGuardsAndNormalises(t *testing.T) {
	c, p := setup(t, "ai_processing")
	h := testkit.Router("owner-a", New(c))
	url := "/profiles/" + p + "/prescriptions/read"
	if code, _ := call(h, "POST", url, "x"); code != http.StatusBadRequest {
		t.Fatalf("no cloud confirmation = %d", code)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"is_prescription":true,"doctor":"Dr B","model":"m","unreadable":[],"medicines":[
			{"name":"Amlodipine","strength":"5 mg","frequency":"OD","timing":"at night","duration":"2 weeks","confidence":0.9},
			{"name":"Cough syrup","frequency":"??","confidence":0.3}]}`))
	}))
	defer fake.Close()
	t.Setenv("AI_SERVICE_URL", fake.URL)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", url+"?cloud=1", strings.NewReader("img"))
	req.Header.Set("Content-Type", "image/png")
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("read = %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Items []item `json:"items"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	items := out.Data.Items
	if len(items) != 2 || items[0].Slots[0] != "night" || items[0].DurationDays == nil || *items[0].DurationDays != 14 || items[1].Understood {
		t.Fatalf("items = %+v", items)
	}
	if testkit.Count(t, c, `SELECT count(*) FROM prescriptions`) != 0 {
		t.Fatal("reading must not save anything")
	}
}
