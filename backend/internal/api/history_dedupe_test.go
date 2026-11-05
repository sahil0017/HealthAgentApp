package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSameHistoryEntryIsKeptOnce(t *testing.T) {
	const ownerID = "00000000-0000-0000-0000-0000000000a6"
	a, h := newAPI(t, ownerID, nil)
	profileID := seedProfile(t, a, ownerID, "k/hist-dupe")
	if _, err := a.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'medical_record_storage',true)`, profileID); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, title := range []string{"Type 2 diabetes", "type 2  Diabetes!", "TYPE 2 DIABETES"} {
		w := call(h, "POST", "/profiles/"+profileID+"/history", `{"kind":"condition","title":"`+title+`"}`)
		if w.Code >= 300 {
			t.Fatalf("create %q: %d %s", title, w.Code, w.Body)
		}
		var env struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		ids[env.Data.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("the same condition must resolve to one entry, got %d", len(ids))
	}
	w := call(h, "GET", "/profiles/"+profileID+"/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Data) != 1 || list.Data[0]["source"] != "user" {
		t.Fatalf("want one user entry, got %s", w.Body)
	}
}
