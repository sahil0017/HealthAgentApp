package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Cross-module flow: device data appears in the governance export without internal dedupe keys,
// and is erased with the profile.
func TestDeviceDataIsExportedAndDeletedWithTheProfile(t *testing.T) {
	c, h := newAPI(t, "device-flow-owner", &fakeRemover{})
	profile := seedProfile(t, c, "device-flow-owner")
	if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'health_data_integration',true)`, profile); err != nil {
		t.Fatal(err)
	}
	w := call(h, "POST", "/profiles/"+profile+"/devices", `{"kind":"smartwatch","source":"healthkit","name":"Watch","external_id":"flow-1"}`)
	var created struct {
		Data struct{ ID string } `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	when := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"readings":[{"metric":"heart_rate","value":71,"unit":"bpm","measured_at":%q,"external_id":"secret-key"}]}`, when)
	if w := call(h, "POST", "/profiles/"+profile+"/devices/"+created.Data.ID+"/readings", body); w.Code != 200 {
		t.Fatalf("sync: %d %s", w.Code, w.Body)
	}

	w = call(h, "GET", "/profiles/"+profile+"/export", "")
	var export struct {
		Data struct {
			Devices  []map[string]any `json:"devices"`
			Readings []map[string]any `json:"device_readings"`
			Syncs    []map[string]any `json:"device_syncs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil || w.Code != 200 || len(export.Data.Devices) != 1 || len(export.Data.Readings) != 1 || len(export.Data.Syncs) != 1 {
		t.Fatalf("export must include devices, readings and syncs: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "secret-key") {
		t.Fatal("internal dedupe keys must not be exported")
	}

	if w := call(h, "DELETE", "/profiles/"+profile, `{"confirm":"DELETE"}`); w.Code != 200 && w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, strings.TrimSpace(w.Body.String()))
	}
	if n := count(t, c, `SELECT (SELECT count(*) FROM devices WHERE profile_id=$1)+(SELECT count(*) FROM device_readings WHERE profile_id=$1)+(SELECT count(*) FROM device_syncs WHERE profile_id=$1)`, profile); n != 0 {
		t.Fatalf("device data survived profile deletion: %d", n)
	}
}
