package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeneralAnswerConsentOwnershipAndQuestionOnly(t *testing.T) {
	f := newInvFixture(t)
	base := "/profiles/" + f.profile + "/investigations"
	code, inv := f.do("owner-a", "POST", base, `{"question":"What is vitamin D?"}`)
	if code != 201 {
		t.Fatalf("create: %d %v", code, inv)
	}
	path := base + "/" + inv["id"].(string) + "/general-answer"
	if code, _ := f.do("owner-b", "POST", path, ""); code != 404 {
		t.Fatalf("other owner: %d", code)
	}
	if code, _ := f.do("owner-a", "POST", path, ""); code != 403 {
		t.Fatalf("without consent: %d", code)
	}
	ctx := context.Background()
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'ai_processing',true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, f.profile); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.do("owner-a", "POST", path, ""); code != 403 {
		t.Fatalf("without cloud permission: %d", code)
	}
	if _, err := f.c.DB.Pool.Exec(ctx, `INSERT INTO agent_permissions(profile_id,owner_id,permission,granted) VALUES($1,'owner-a','cloud_model',true) ON CONFLICT (profile_id,permission) DO UPDATE SET granted=true`, f.profile); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if r.URL.Path != "/v1/research/general-answer" || json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload) != 1 || payload["question"] != "What is vitamin D?" {
			t.Errorf("unexpected request %s: %v", r.URL.Path, payload)
		}
		_, _ = w.Write([]byte(`{"answer":"General information.","source":"copilot"}`))
	}))
	defer srv.Close()
	t.Setenv("AI_SERVICE_URL", srv.URL)
	if code, result := f.do("owner-a", "POST", path, ""); code != 200 || result["answer"] != "General information." || result["source"] != "copilot" {
		t.Fatalf("answer: %d %v", code, result)
	}
}
