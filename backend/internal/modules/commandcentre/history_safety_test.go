package commandcentre

import (
	"context"
	"encoding/json"
	"testing"

	"cortex/backend/internal/platform/testkit"
)

func TestManualHistoryReplacesLegacySuggestion(t *testing.T) {
	c := testkit.Open(t)
	profile := testkit.SeedProfile(t, c, "owner-a")
	if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'health_data',true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, profile); err != nil {
		t.Fatal(err)
	}
	_, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO history_entries(profile_id,owner_id,kind,title,notes,source)
		VALUES($1,'owner-a','condition','Asthma','AI inference','report')`, profile)
	if err != nil {
		t.Fatal(err)
	}
	w := testkit.Call(testkit.Router("owner-a", New(c)), "POST", "/profiles/"+profile+"/history", `{"kind":"condition","title":"Asthma","notes":"Confirmed by my clinician"}`)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var result struct {
		Data HistoryEntry `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Source != "user" || result.Data.Notes != "Confirmed by my clinician" {
		t.Fatalf("history: %+v", result.Data)
	}
}
