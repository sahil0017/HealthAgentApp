package health

import (
	"cortex/backend/internal/domain/biomarker"
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
	target := GoalTarget{Max: f(5.7), Unit: "%"}
	cases := []struct {
		name             string
		latest, previous *biomarker.Reading
		status, trend    string
	}{
		{"no data", nil, nil, "no_data", "unknown"},
		{"unit mismatch is never compared", &biomarker.Reading{Value: 40, Unit: "mmol/mol"}, nil, "unit_mismatch", "unknown"},
		{"on target without history", &biomarker.Reading{Value: 5.4, Unit: "%"}, nil, "on_target", "unknown"},
		{"above target improving", &biomarker.Reading{Value: 6.1, Unit: "%"}, &biomarker.Reading{Value: 6.8, Unit: "%"}, "above_target", "improving"},
		{"above target worsening", &biomarker.Reading{Value: 7.0, Unit: "%"}, &biomarker.Reading{Value: 6.1, Unit: "%"}, "above_target", "worsening"},
		{"unit case is ignored", &biomarker.Reading{Value: 5.0, Unit: " % "}, &biomarker.Reading{Value: 5.2, Unit: "%"}, "on_target", "steady"},
		{"previous in other unit gives unknown trend", &biomarker.Reading{Value: 6.0, Unit: "%"}, &biomarker.Reading{Value: 40, Unit: "mmol/mol"}, "above_target", "unknown"},
	}
	for _, c := range cases {
		got := EvaluateGoal(target, c.latest, c.previous)
		if got.Status != c.status || got.Trend != c.trend {
			t.Errorf("%s: got %+v, want %s/%s", c.name, got, c.status, c.trend)
		}
	}
}

func TestEvaluateGoalWithMinimumAndRange(t *testing.T) {
	floor := GoalTarget{Min: f(40), Unit: "mg/dL"}
	if got := EvaluateGoal(floor, &biomarker.Reading{Value: 35, Unit: "mg/dL"}, &biomarker.Reading{Value: 30, Unit: "mg/dL"}); got.Status != "below_target" || got.Trend != "improving" {
		t.Errorf("floor: %+v", got)
	}
	band := GoalTarget{Min: f(70), Max: f(99), Unit: "mg/dL"}
	if got := EvaluateGoal(band, &biomarker.Reading{Value: 85, Unit: "mg/dL"}, &biomarker.Reading{Value: 90, Unit: "mg/dL"}); got.Status != "on_target" || got.Trend != "steady" {
		t.Errorf("inside band should be steady: %+v", got)
	}
	if got := EvaluateGoal(band, &biomarker.Reading{Value: 105, Unit: "mg/dL"}, &biomarker.Reading{Value: 85, Unit: "mg/dL"}); got.Status != "above_target" || got.Trend != "worsening" {
		t.Errorf("leaving band: %+v", got)
	}
}

func TestSummarizeChange(t *testing.T) {
	if _, ok := SummarizeChange("X", "X", nil); ok {
		t.Fatal("no readings means no summary")
	}
	got, _ := SummarizeChange("GLUCOSE", "Glucose", []biomarker.Reading{{Value: 100, Unit: "mg/dL", On: day("2026-01-01")}, {Value: 110, Unit: "mg/dL", On: day("2026-02-01")}})
	if got.Value != 110 || got.Change == nil || *got.Change != 10 || got.ChangePercent == nil || *got.ChangePercent != 10 || got.TrackedReading != 2 {
		t.Fatalf("unexpected change: %+v", got)
	}
	mixed, _ := SummarizeChange("GLUCOSE", "Glucose", []biomarker.Reading{{Value: 5.5, Unit: "mmol/L", On: day("2026-01-01")}, {Value: 110, Unit: "mg/dL", On: day("2026-02-01")}})
	if mixed.Change != nil || mixed.PreviousValue == nil {
		t.Fatalf("a change must not be computed across units: %+v", mixed)
	}
	zero, _ := SummarizeChange("X", "X", []biomarker.Reading{{Value: 0, Unit: "u", On: day("2026-01-01")}, {Value: 3, Unit: "u", On: day("2026-02-01")}})
	if zero.Change == nil || zero.ChangePercent != nil {
		t.Fatalf("percent change from zero is undefined: %+v", zero)
	}
	single, _ := SummarizeChange("X", "X", []biomarker.Reading{{Value: 1, Unit: "u", On: day("2026-01-01")}})
	if single.PreviousValue != nil || single.Change != nil {
		t.Fatalf("a single reading has no change: %+v", single)
	}
}

func TestAssessCompleteness(t *testing.T) {
	empty := AssessCompleteness(CompletenessFacts{})
	if empty.Score != 0 || len(empty.Items) != 7 {
		t.Fatalf("empty record: %+v", empty)
	}
	full := AssessCompleteness(CompletenessFacts{StorageConsent: true, ProcessingConsent: true, Documents: 2, HistoryEntries: 1, ActiveGoals: 1, ProfileDetails: true})
	if full.Score != 100 {
		t.Fatalf("everything done should score 100, got %d", full.Score)
	}
	pending := AssessCompleteness(CompletenessFacts{StorageConsent: true, ProcessingConsent: true, Documents: 1, DocumentsToReview: 1, ObservationsPending: 2, HistoryEntries: 1, ActiveGoals: 1, ProfileDetails: true})
	for _, item := range pending.Items {
		if (item.Key == "reviewed" || item.Key == "verified") && item.Done {
			t.Errorf("%s must not be done while items await review", item.Key)
		}
	}
	if pending.Score != 71 {
		t.Fatalf("5 of 7 should score 71, got %d", pending.Score)
	}
	consentOnly := AssessCompleteness(CompletenessFacts{StorageConsent: true})
	if consentOnly.Items[0].Done {
		t.Error("consent needs both storage and processing")
	}
}
