package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Cross-module flow: a saved health question appears in the governance export and is erased with the profile.
func TestHealthQuestionsAreExportedAndDeletedWithTheProfile(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/answer" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"answer":"ok","sources":[],"limitations":[],"workflow":[],"urgent":false,"triage":"none","confidence":"insufficient","uncertainty":"","suggested_questions":[]}`))
	}))
	defer ai.Close()
	t.Setenv("AI_SERVICE_URL", ai.URL)

	c, h := newAPI(t, "flow-owner", &fakeRemover{})
	profile := seedProfile(t, c, "flow-owner")
	if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'medical_record_storage',true)`, profile); err != nil {
		t.Fatal(err)
	}
	if w := call(h, "POST", "/profiles/"+profile+"/questions", `{"question":"What does my record say?"}`); w.Code != 200 {
		t.Fatalf("ask: %d %s", w.Code, w.Body)
	}

	w := call(h, "GET", "/profiles/"+profile+"/export", "")
	var export struct {
		Data struct {
			Questions []map[string]any `json:"health_questions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil || w.Code != 200 || len(export.Data.Questions) != 1 || export.Data.Questions[0]["question"] != "What does my record say?" {
		t.Fatalf("export must include the saved question: %d %s", w.Code, w.Body)
	}

	if w := call(h, "DELETE", "/profiles/"+profile, `{"confirm":"DELETE"}`); w.Code != 200 && w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, strings.TrimSpace(w.Body.String()))
	}
	if n := count(t, c, `SELECT count(*) FROM health_questions WHERE profile_id=$1`, profile); n != 0 {
		t.Fatalf("saved questions survived profile deletion: %d", n)
	}
}
