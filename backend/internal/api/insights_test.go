package api

import (
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

func TestEvaluateGoalStatusAndTrend(t *testing.T) {
	target := goalTarget{Max: f(5.7), Unit: "%"}
	cases := []struct {
		name             string
		latest, previous *reading
		status, trend    string
	}{
		{"no data", nil, nil, "no_data", "unknown"},
		{"unit mismatch is never compared", &reading{Value: 40, Unit: "mmol/mol"}, nil, "unit_mismatch", "unknown"},
		{"on target without history", &reading{Value: 5.4, Unit: "%"}, nil, "on_target", "unknown"},
		{"above target improving", &reading{Value: 6.1, Unit: "%"}, &reading{Value: 6.8, Unit: "%"}, "above_target", "improving"},
		{"above target worsening", &reading{Value: 7.0, Unit: "%"}, &reading{Value: 6.1, Unit: "%"}, "above_target", "worsening"},
		{"unit case is ignored", &reading{Value: 5.0, Unit: " % "}, &reading{Value: 5.2, Unit: "%"}, "on_target", "steady"},
		{"previous in other unit gives unknown trend", &reading{Value: 6.0, Unit: "%"}, &reading{Value: 40, Unit: "mmol/mol"}, "above_target", "unknown"},
	}
	for _, c := range cases {
		got := evaluateGoal(target, c.latest, c.previous)
		if got.Status != c.status || got.Trend != c.trend {
			t.Errorf("%s: got %+v, want %s/%s", c.name, got, c.status, c.trend)
		}
	}
}

func TestEvaluateGoalWithMinimumAndRange(t *testing.T) {
	floor := goalTarget{Min: f(40), Unit: "mg/dL"}
	if got := evaluateGoal(floor, &reading{Value: 35, Unit: "mg/dL"}, &reading{Value: 30, Unit: "mg/dL"}); got.Status != "below_target" || got.Trend != "improving" {
		t.Errorf("floor: %+v", got)
	}
	band := goalTarget{Min: f(70), Max: f(99), Unit: "mg/dL"}
	if got := evaluateGoal(band, &reading{Value: 85, Unit: "mg/dL"}, &reading{Value: 90, Unit: "mg/dL"}); got.Status != "on_target" || got.Trend != "steady" {
		t.Errorf("inside band should be steady: %+v", got)
	}
	if got := evaluateGoal(band, &reading{Value: 105, Unit: "mg/dL"}, &reading{Value: 85, Unit: "mg/dL"}); got.Status != "above_target" || got.Trend != "worsening" {
		t.Errorf("leaving band: %+v", got)
	}
}

func TestMeasurementCadenceUsesTheMedianGapOfDistinctDays(t *testing.T) {
	if _, ok := measurementCadence([]time.Time{day("2026-01-01"), day("2026-02-01")}); ok {
		t.Fatal("two dates are not enough to learn a cadence")
	}
	if _, ok := measurementCadence([]time.Time{day("2026-01-01"), day("2026-01-01"), day("2026-01-01")}); ok {
		t.Fatal("repeated readings on one day are a single date")
	}
	got, ok := measurementCadence([]time.Time{day("2026-01-01"), day("2026-01-31"), day("2026-03-02"), day("2026-06-30")})
	if !ok || got != 30*24*time.Hour {
		t.Fatalf("median of 30,30,120 days should be 30 days, got %v %v", got, ok)
	}
	even, _ := measurementCadence([]time.Time{day("2026-01-01"), day("2026-01-11"), day("2026-02-10")})
	if even != 20*24*time.Hour {
		t.Fatalf("median of 10,30 days should be 20 days, got %v", even)
	}
}

func TestOverdueForMeasurementNeedsMoreThanTwiceTheUsualGap(t *testing.T) {
	cadence := 30 * 24 * time.Hour
	last := day("2026-01-01")
	if overdueForMeasurement(last, day("2026-03-02"), cadence) {
		t.Error("60 days is exactly twice the gap and not yet overdue")
	}
	if !overdueForMeasurement(last, day("2026-03-03"), cadence) {
		t.Error("61 days should be overdue")
	}
	if overdueForMeasurement(last, day("2030-01-01"), 0) {
		t.Error("without a cadence nothing is overdue")
	}
}

func TestSummarizeChange(t *testing.T) {
	if _, ok := summarizeChange("X", "X", nil); ok {
		t.Fatal("no readings means no summary")
	}
	got, _ := summarizeChange("GLUCOSE", "Glucose", []reading{{Value: 100, Unit: "mg/dL", On: day("2026-01-01")}, {Value: 110, Unit: "mg/dL", On: day("2026-02-01")}})
	if got.Value != 110 || got.Change == nil || *got.Change != 10 || got.ChangePercent == nil || *got.ChangePercent != 10 || got.TrackedReading != 2 {
		t.Fatalf("unexpected change: %+v", got)
	}
	mixed, _ := summarizeChange("GLUCOSE", "Glucose", []reading{{Value: 5.5, Unit: "mmol/L", On: day("2026-01-01")}, {Value: 110, Unit: "mg/dL", On: day("2026-02-01")}})
	if mixed.Change != nil || mixed.PreviousValue == nil {
		t.Fatalf("a change must not be computed across units: %+v", mixed)
	}
	zero, _ := summarizeChange("X", "X", []reading{{Value: 0, Unit: "u", On: day("2026-01-01")}, {Value: 3, Unit: "u", On: day("2026-02-01")}})
	if zero.Change == nil || zero.ChangePercent != nil {
		t.Fatalf("percent change from zero is undefined: %+v", zero)
	}
	single, _ := summarizeChange("X", "X", []reading{{Value: 1, Unit: "u", On: day("2026-01-01")}})
	if single.PreviousValue != nil || single.Change != nil {
		t.Fatalf("a single reading has no change: %+v", single)
	}
}

func TestAssessCompleteness(t *testing.T) {
	empty := assessCompleteness(completenessFacts{})
	if empty.Score != 0 || len(empty.Items) != 7 {
		t.Fatalf("empty record: %+v", empty)
	}
	full := assessCompleteness(completenessFacts{StorageConsent: true, ProcessingConsent: true, Documents: 2, HistoryEntries: 1, ActiveGoals: 1, ProfileDetails: true})
	if full.Score != 100 {
		t.Fatalf("everything done should score 100, got %d", full.Score)
	}
	pending := assessCompleteness(completenessFacts{StorageConsent: true, ProcessingConsent: true, Documents: 1, DocumentsToReview: 1, ObservationsPending: 2, HistoryEntries: 1, ActiveGoals: 1, ProfileDetails: true})
	for _, item := range pending.Items {
		if (item.Key == "reviewed" || item.Key == "verified") && item.Done {
			t.Errorf("%s must not be done while items await review", item.Key)
		}
	}
	if pending.Score != 71 {
		t.Fatalf("5 of 7 should score 71, got %d", pending.Score)
	}
	consentOnly := assessCompleteness(completenessFacts{StorageConsent: true})
	if consentOnly.Items[0].Done {
		t.Error("consent needs both storage and processing")
	}
}

func TestParseDayAndValidators(t *testing.T) {
	good, bad, blank := "2026-02-03", "03/02/2026", "  "
	if got, err := parseDay(&good); err != nil || *got != good {
		t.Errorf("valid date: %v %v", got, err)
	}
	if _, err := parseDay(&bad); err == nil {
		t.Error("non ISO date must fail")
	}
	if got, err := parseDay(&blank); err != nil || got != nil {
		t.Errorf("blank date means unset: %v %v", got, err)
	}
	in := historyInput{}
	if field, _ := in.validate(true); field == "" {
		t.Error("creating requires kind and title")
	}
	kind, title := "wizardry", "x"
	in = historyInput{Kind: &kind, Title: &title}
	if field, _ := in.validate(true); field != "kind" {
		t.Errorf("unsupported kind should be rejected, got %q", field)
	}
	kind, title = "allergy", "   "
	in = historyInput{Kind: &kind, Title: &title}
	if field, _ := in.validate(true); field != "title" {
		t.Errorf("blank title should be rejected, got %q", field)
	}
	status := "paused"
	fu := followUpInput{Status: &status}
	if field, _ := fu.validate(false); field != "status" {
		t.Errorf("unknown status should be rejected, got %q", field)
	}
}

func TestWithoutExistingAndAge(t *testing.T) {
	got := withoutExisting([]suggestion{{Title: "Repeat HbA1c"}, {Title: "repeat hba1c "}, {Title: "Recheck LDL"}}, []string{"Recheck ldl"})
	if len(got) != 1 || got[0].Title != "Repeat HbA1c" {
		t.Fatalf("duplicates and open follow-ups must be dropped: %+v", got)
	}
	b := day("2000-06-15")
	if ageYears(b, day("2026-06-14")) != 25 || ageYears(b, day("2026-06-15")) != 26 {
		t.Error("age must roll over on the birthday")
	}
}
