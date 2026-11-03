package commandcentre

import (
	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/domain/health"
	"cortex/backend/internal/platform/httpx"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseDayAndValidators(t *testing.T) {
	good, bad, blank := "2026-02-03", "03/02/2026", "  "
	if got, err := httpx.ParseDay(&good); err != nil || *got != good {
		t.Errorf("valid date: %v %v", got, err)
	}
	if _, err := httpx.ParseDay(&bad); err == nil {
		t.Error("non ISO date must fail")
	}
	if got, err := httpx.ParseDay(&blank); err != nil || got != nil {
		t.Errorf("blank date means unset: %v %v", got, err)
	}
	in := HistoryInput{}
	if field, _ := in.validate(true); field == "" {
		t.Error("creating requires kind and title")
	}
	kind, title := "wizardry", "x"
	in = HistoryInput{Kind: &kind, Title: &title}
	if field, _ := in.validate(true); field != "kind" {
		t.Errorf("unsupported kind should be rejected, got %q", field)
	}
	kind, title = "allergy", "   "
	in = HistoryInput{Kind: &kind, Title: &title}
	if field, _ := in.validate(true); field != "title" {
		t.Errorf("blank title should be rejected, got %q", field)
	}
	status := "paused"
	fu := FollowUpInput{Status: &status}
	if field, _ := fu.validate(false); field != "status" {
		t.Errorf("unknown status should be rejected, got %q", field)
	}
}

func TestWithoutExistingAndAge(t *testing.T) {
	got := health.WithoutExisting([]health.Suggestion{{Title: "Repeat HbA1c"}, {Title: "repeat hba1c "}, {Title: "Recheck LDL"}}, []string{"Recheck ldl"})
	if len(got) != 1 || got[0].Title != "Repeat HbA1c" {
		t.Fatalf("duplicates and open follow-ups must be dropped: %+v", got)
	}
	b := day("2000-06-15")
	if biomarker.AgeYears(b, day("2026-06-14")) != 25 || biomarker.AgeYears(b, day("2026-06-15")) != 26 {
		t.Error("age must roll over on the birthday")
	}
}
