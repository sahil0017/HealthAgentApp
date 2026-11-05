package research

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"
)

type invFixture struct {
	t       *testing.T
	c       *core.Core
	profile string
	source  string
}

func newInvFixture(t *testing.T) *invFixture {
	t.Helper()
	c := testkit.Open(t)
	f := &invFixture{t: t, c: c, profile: testkit.SeedProfile(t, c, "owner-a")}
	ctx := context.Background()
	var entry string
	if err := c.DB.Pool.QueryRow(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload,status) VALUES('research_source',$1,'{}','approved') RETURNING id::text`, "inv-"+f.profile).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = c.DB.Pool.Exec(ctx, `DELETE FROM knowledge_entries WHERE id=$1`, entry) })
	if err := c.DB.Pool.QueryRow(ctx, `INSERT INTO research_sources(knowledge_entry_id,identifier,title,url) VALUES($1,'PMID:1','Ferritin and fatigue','https://pubmed.ncbi.nlm.nih.gov/1/') RETURNING id::text`, entry).Scan(&f.source); err != nil {
		t.Fatal(err)
	}
	vec := "[" + strings.TrimSuffix(strings.Repeat("0.1,", 384), ",") + "]"
	if _, err := c.DB.Pool.Exec(ctx, `INSERT INTO research_chunks(source_id,ordinal,content,embedding,embedding_model) VALUES($1,0,'Low ferritin is associated with fatigue in adults.',$2::vector,'test')`, f.source, vec); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *invFixture) do(owner, method, path, body string) (int, map[string]any) {
	f.t.Helper()
	w := testkit.Call(testkit.Router(owner, New(f.c)), method, path, body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if data, ok := out["data"].(map[string]any); ok {
		out = data
	}
	return w.Code, out
}

func TestInvestigationLifecycle(t *testing.T) {
	f := newInvFixture(t)
	base := "/profiles/" + f.profile + "/investigations"
	for _, body := range []string{`{}`, `{"question":"x"}`, `{"question":"` + strings.Repeat("q", 501) + `"}`} {
		if code, _ := f.do("owner-a", "POST", base, body); code != 400 {
			t.Fatalf("%s: %d", body[:min(len(body), 30)], code)
		}
	}
	if code, _ := f.do("owner-b", "POST", base, `{"question":"Does ferritin affect fatigue?"}`); code != 404 {
		t.Fatalf("other owner: %d", code)
	}
	code, inv := f.do("owner-a", "POST", base, `{"question":"Does ferritin affect fatigue?"}`)
	if code != 201 || inv["title"] != "Does ferritin affect fatigue?" || inv["status"] != "open" {
		t.Fatalf("create: %d %v", code, inv)
	}
	id := inv["id"].(string)
	one := base + "/" + id

	if code, out := f.do("owner-a", "POST", one+"/refresh", ""); code != 200 || out["added"].(float64) < 1 {
		t.Fatalf("refresh: %d %v", code, out)
	}
	if code, out := f.do("owner-a", "POST", one+"/refresh", ""); code != 200 || out["added"] != float64(0) {
		t.Fatalf("refresh again must not duplicate: %v", out)
	}
	if code, _ := f.do("owner-a", "POST", one+"/items", `{"source_id":"`+f.source+`"}`); code != 409 {
		t.Fatalf("duplicate source: %d", code)
	}
	for _, body := range []string{`{}`, `{"source_id":"` + f.source + `","note":"both"}`, `{"source_id":"not-a-uuid"}`, `{"source_id":"00000000-0000-0000-0000-000000000000"}`} {
		if code, _ := f.do("owner-a", "POST", one+"/items", body); code != 400 && code != 404 {
			t.Fatalf("%s: %d", body, code)
		}
	}
	code, note := f.do("owner-a", "POST", one+"/items", `{"note":"Ask the GP about iron studies"}`)
	if code != 201 {
		t.Fatalf("note: %d", code)
	}

	code, got := f.do("owner-a", "GET", one, "")
	items := got["items"].([]any)
	if code != 200 || len(items) < 2 || got["disclaimer"] == nil {
		t.Fatalf("get: %d %v", code, got)
	}
	found := false
	for _, it := range items {
		if m := it.(map[string]any); m["source_id"] == f.source && m["identifier"] == "PMID:1" && m["url"] != nil && m["excerpt"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("source item missing: %v", items)
	}
	if code, _ := f.do("owner-b", "GET", one, ""); code != 404 {
		t.Fatalf("other owner get: %d", code)
	}

	if code, out := f.do("owner-a", "PATCH", one, `{"status":"archived","title":"Iron and energy"}`); code != 200 || out["status"] != "archived" || out["title"] != "Iron and energy" || out["source_count"].(float64) < 1 || out["note_count"] != float64(1) {
		t.Fatalf("update: %d %v", code, out)
	}
	for _, body := range []string{`{}`, `{"status":"bogus"}`, `{"title":" "}`, `{"question":"x"}`} {
		if code, _ := f.do("owner-a", "PATCH", one, body); code != 400 {
			t.Fatalf("patch %s: %d", body, code)
		}
	}
	if code, out := f.do("owner-a", "GET", base+"?status=open", ""); code != 200 || len(out["investigations"].([]any)) != 0 {
		t.Fatalf("open filter: %v", out)
	}
	if code, out := f.do("owner-a", "GET", base+"?status=archived", ""); code != 200 || len(out["investigations"].([]any)) != 1 {
		t.Fatalf("archived filter: %v", out)
	}
	if code, _ := f.do("owner-a", "GET", base+"?status=zzz", ""); code != 400 {
		t.Fatalf("bad filter: %d", code)
	}

	noteID := note["id"].(string)
	if code, _ := f.do("owner-a", "DELETE", one+"/items/"+noteID, ""); code != 204 {
		t.Fatalf("delete item: %d", code)
	}
	if code, _ := f.do("owner-a", "DELETE", one+"/items/"+noteID, ""); code != 404 {
		t.Fatalf("delete item twice: %d", code)
	}
	if code, _ := f.do("owner-a", "DELETE", one+"/items/nope", ""); code != 404 {
		t.Fatalf("delete bad item: %d", code)
	}

	f.do("owner-a", "POST", base, `{"title":"Second","question":"vitamin d and bones"}`)
	if code, _ := f.do("owner-b", "DELETE", one, ""); code != 404 {
		t.Fatalf("other owner delete: %d", code)
	}
	if code, _ := f.do("owner-a", "DELETE", one, ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := f.do("owner-a", "GET", one, ""); code != 404 {
		t.Fatalf("deleted get: %d", code)
	}
	if code, _ := f.do("owner-a", "GET", base+"/nope", ""); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM audit_events WHERE profile_id=$1 AND action LIKE 'research.investigation_%'`, f.profile); n < 5 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestInvestigationsAreErasedWithTheProfile(t *testing.T) {
	f := newInvFixture(t)
	f.do("owner-a", "POST", "/profiles/"+f.profile+"/investigations", `{"question":"ferritin fatigue"}`)
	if _, err := f.c.DB.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE id=$1`, f.profile); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, f.c, `SELECT count(*) FROM research_investigations WHERE profile_id=$1`, f.profile); n != 0 {
		t.Fatalf("investigations left behind: %d", n)
	}
}

func TestFollowingTheSameQuestionTwiceKeepsOneInvestigation(t *testing.T) {
	f := newInvFixture(t)
	base := "/profiles/" + f.profile + "/investigations"
	_, first := f.do("owner-a", "POST", base, `{"question":"Does ferritin affect fatigue?"}`)
	code, second := f.do("owner-a", "POST", base, `{"question":"  does FERRITIN affect fatigue? "}`)
	if code != 200 || second["id"] != first["id"] {
		t.Fatalf("expected the existing investigation back, got %d %v vs %v", code, second["id"], first["id"])
	}
	_, list := f.do("owner-a", "GET", base, "")
	if items, _ := list["investigations"].([]any); len(items) != 1 {
		t.Fatalf("expected one investigation, got %v", list)
	}
}
