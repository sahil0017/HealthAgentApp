package healthai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cortex/backend/internal/platform/testkit"

	"github.com/google/uuid"
)

const fakeAnswer = `{"answer":"Your glucose was 95 mg/dL.","sources":[{"type":"user_record","description":"Glucose 95"}],"limitations":["Educational only."],
"workflow":["medical_records"],"urgent":false,"triage":"none","confidence":"high","uncertainty":"Based on your approved records.","suggested_questions":["How has my glucose changed?"]}`

type fixture struct {
	h       http.Handler
	profile string
	t       *testing.T
	ai      *httptest.Server
}

func newFixture(t *testing.T, aiBody string, aiStatus int) *fixture {
	t.Helper()
	c := testkit.Open(t)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/answer" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(aiStatus)
		_, _ = w.Write([]byte(aiBody))
	}))
	t.Cleanup(ai.Close)
	t.Setenv("AI_SERVICE_URL", ai.URL)
	profile := testkit.SeedProfile(t, c, "owner-a", "k/"+uuid.NewString())
	if _, err := c.DB.Pool.Exec(context.Background(), `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'medical_record_storage',true)`, profile); err != nil {
		t.Fatal(err)
	}
	return &fixture{h: testkit.Router("owner-a", New(c)), profile: profile, t: t, ai: ai}
}

func (f *fixture) call(method, path, body string) (int, map[string]any) {
	w := testkit.Call(f.h, method, "/profiles/"+f.profile+path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (f *fixture) ask(question string) (int, map[string]any) {
	body, _ := json.Marshal(map[string]string{"question": question})
	return f.call("POST", "/questions", string(body))
}

func data(m map[string]any) map[string]any { d, _ := m["data"].(map[string]any); return d }

func TestAskIsSavedListedAndAudited(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	code, out := f.ask("  What was my glucose?  ")
	if code != 200 {
		t.Fatalf("ask: %d %v", code, out)
	}
	answer := data(out)
	id, _ := answer["id"].(string)
	if id == "" || answer["question"] != "What was my glucose?" || answer["confidence"] != "high" || answer["created_at"] == nil {
		t.Fatalf("answer not enriched with saved fields: %v", answer)
	}

	f.ask("Second question")
	code, list := f.call("GET", "/questions", "")
	items, _ := data(list)["questions"].([]any)
	if code != 200 || len(items) != 2 {
		t.Fatalf("history: %d %v", code, list)
	}
	if first, _ := items[0].(map[string]any); first["question"] != "Second question" {
		t.Fatalf("history must be newest first: %v", items)
	}
	_, page := f.call("GET", "/questions?limit=1&offset=1", "")
	if paged, _ := data(page)["questions"].([]any); len(paged) != 1 {
		t.Fatalf("pagination: %v", page)
	}
	if code, _ := f.call("GET", "/questions?limit=0", ""); code != 400 {
		t.Fatalf("bad limit: %d", code)
	}

	code, one := f.call("GET", "/questions/"+id, "")
	saved := data(one)
	if code != 200 || saved["answer"] != "Your glucose was 95 mg/dL." || saved["feedback"] != nil {
		t.Fatalf("get: %d %v", code, one)
	}
	if sources, _ := saved["sources"].([]any); len(sources) != 1 {
		t.Fatalf("sources not persisted: %v", saved)
	}

	c := testkit.Open(t)
	if n := testkit.Count(t, c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='question.asked' AND resource_id=$2`, f.profile, id); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}
	var stored string
	if err := c.DB.Pool.QueryRow(context.Background(), `SELECT details::text FROM audit_events WHERE profile_id=$1 AND action='question.asked' AND resource_id=$2`, f.profile, id).Scan(&stored); err != nil || strings.Contains(stored, "glucose") {
		t.Fatalf("audit details must not hold the question text: %q %v", stored, err)
	}
}

func TestFeedbackAndDelete(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	_, out := f.ask("Question")
	id := data(out)["id"].(string)

	if code, _ := f.call("PUT", "/questions/"+id+"/feedback", `{"rating":"great"}`); code != 400 {
		t.Fatalf("invalid rating accepted: %d", code)
	}
	if code, _ := f.call("PUT", "/questions/"+id+"/feedback", `{"rating":"helpful","comment":"`+strings.Repeat("x", 1001)+`"}`); code != 400 {
		t.Fatalf("overlong comment accepted: %d", code)
	}
	code, rated := f.call("PUT", "/questions/"+id+"/feedback", `{"rating":"not_helpful","comment":"  too vague "}`)
	if code != 200 || data(rated)["feedback"] != "not_helpful" || data(rated)["feedback_comment"] != "too vague" {
		t.Fatalf("feedback: %d %v", code, rated)
	}
	if code, _ := f.call("PUT", "/questions/"+id+"/feedback", `{"rating":"helpful"}`); code != 200 {
		t.Fatalf("feedback can be changed: %d", code)
	}
	_, again := f.call("GET", "/questions/"+id, "")
	if data(again)["feedback"] != "helpful" || data(again)["feedback_comment"] != nil {
		t.Fatalf("latest rating must replace the old comment: %v", again)
	}

	if code, _ := f.call("DELETE", "/questions/"+id, ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := f.call("DELETE", "/questions/"+id, ""); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
	if code, _ := f.call("GET", "/questions/"+id, ""); code != 404 {
		t.Fatalf("deleted question still readable: %d", code)
	}
	c := testkit.Open(t)
	if n := testkit.Count(t, c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action='question.deleted'`, f.profile); n != 1 {
		t.Fatalf("deletion not audited: %d", n)
	}
}

func TestQuestionsAreIsolatedByOwner(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	_, out := f.ask("Private question")
	id := data(out)["id"].(string)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/questions", ""}, {"GET", "/questions/" + id, ""},
		{"PUT", "/questions/" + id + "/feedback", `{"rating":"helpful"}`}, {"DELETE", "/questions/" + id, ""},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, "/profiles/"+f.profile+tc.path, strings.NewReader(tc.body))
		req.Header.Set("X-Test-Owner", "owner-b")
		f.h.ServeHTTP(w, req)
		if w.Code != 404 {
			t.Errorf("%s %s as another owner = %d, want 404", tc.method, tc.path, w.Code)
		}
	}
	if code, _ := f.call("GET", "/questions/not-a-uuid", ""); code != 404 {
		t.Fatalf("malformed id = %d", code)
	}
	if code, _ := f.call("GET", "/questions/00000000-0000-0000-0000-000000000000", ""); code != 404 {
		t.Fatalf("unknown id = %d", code)
	}
}

func TestAskValidationAndFailures(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	if code, _ := f.ask("   "); code != 400 {
		t.Fatalf("blank question: %d", code)
	}
	code, out := f.ask(strings.Repeat("é", maxQuestionRunes+1))
	if code != 400 || out["error"].(map[string]any)["code"] != "QUESTION_TOO_LONG" {
		t.Fatalf("overlong question: %d %v", code, out)
	}
	if code, _ := f.ask(strings.Repeat("é", maxQuestionRunes)); code != 200 {
		t.Fatalf("question at the limit rejected: %d", code)
	}

	down := newFixture(t, `{"detail":"boom"}`, 500)
	if code, _ := down.ask("Will this save?"); code != 502 {
		t.Fatalf("AI failure = %d, want 502", code)
	}
	if _, list := down.call("GET", "/questions", ""); len(data(list)["questions"].([]any)) != 0 {
		t.Fatalf("a failed answer must not be saved: %v", list)
	}
}

func TestAskRequiresConsent(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	c := testkit.Open(t)
	if _, err := c.DB.Pool.Exec(context.Background(), `UPDATE consents SET granted=false WHERE profile_id=$1 AND purpose='ai_processing'`, f.profile); err != nil {
		t.Fatal(err)
	}
	if code, _ := f.ask("Anything"); code != 403 {
		t.Fatalf("ask without AI consent = %d", code)
	}
}

func TestUrgentAnswerIsStoredAndUnknownValuesAreNormalised(t *testing.T) {
	body := `{"answer":"Call emergency services.","sources":null,"limitations":[],"workflow":[],"urgent":true,"triage":"emergency","confidence":"not_applicable","uncertainty":"skipped","suggested_questions":[]}`
	f := newFixture(t, body, 200)
	_, out := f.ask("I have chest pain")
	saved := data(out)
	if saved["urgent"] != true || saved["triage"] != "emergency" {
		t.Fatalf("urgent flag lost: %v", saved)
	}
	_, one := f.call("GET", "/questions/"+saved["id"].(string), "")
	if d := data(one); d["urgent"] != true || d["confidence"] != "not_applicable" || d["sources"] == nil {
		t.Fatalf("stored urgent answer: %v", d)
	}

	odd := newFixture(t, `{"answer":"x","triage":"weird","confidence":"certain"}`, 200)
	_, out = odd.ask("Odd values")
	_, one = odd.call("GET", "/questions/"+data(out)["id"].(string), "")
	if d := data(one); d["triage"] != "none" || d["confidence"] != "insufficient" {
		t.Fatalf("unknown triage/confidence must fall back safely: %v", d)
	}
}

func TestProfileDeletionRemovesQuestions(t *testing.T) {
	f := newFixture(t, fakeAnswer, 200)
	f.ask("Will be removed")
	c := testkit.Open(t)
	if _, err := c.DB.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, f.profile); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, c, `SELECT count(*) FROM health_questions WHERE profile_id=$1`, f.profile); n != 0 {
		t.Fatalf("questions survived profile deletion: %d", n)
	}
}
