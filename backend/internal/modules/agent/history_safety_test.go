package agent

import (
	"context"
	"strings"
	"testing"
)

func TestVisitPrepExcludesUnverifiedReportHistory(t *testing.T) {
	f := newFixture(t)
	_, err := f.c.DB.Pool.Exec(context.Background(), `INSERT INTO history_entries(profile_id,owner_id,kind,title,source)
		VALUES($1,'owner-a','condition','Unverified inference','report'),($1,'owner-a','condition','User condition','user')`, f.profile)
	if err != nil {
		t.Fatal(err)
	}
	code, out := f.do("GET", "/agent/visit-prep", "")
	if code != 200 {
		t.Fatalf("visit prep: %d %v", code, out)
	}
	conditions := data(out)["conditions"].([]any)
	if len(conditions) != 1 || conditions[0] != "User condition" {
		t.Fatalf("conditions: %v", conditions)
	}
	titles := New(f.c).historyTitles(context.Background(), "owner-a", f.profile)
	if strings.Contains(strings.Join(titles, " "), "Unverified inference") {
		t.Fatalf("unverified history in AI context: %v", titles)
	}
}
