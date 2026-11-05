package agent

import "testing"

func TestReportsAreFoundByLatestPreviousLabAndID(t *testing.T) {
	l := &labs{reports: []*report{
		{ID: "b", Title: "Orange Health", Date: "2026-03-12"},
		{ID: "a", Title: "Thyrocare", Date: "2025-11-02"},
	}}
	for q, want := range map[string]string{"latest": "b", "previous": "a", "thyrocare": "a", "my Orange Health report": "b", "2025-11": "a", "a": "a"} {
		r, ok := l.findReport(q)
		if !ok || r.ID != want {
			t.Fatalf("%q resolved to %v, want %s", q, r, want)
		}
	}
	if _, ok := l.findReport("apollo"); ok {
		t.Fatal("unknown lab must not resolve")
	}
}
