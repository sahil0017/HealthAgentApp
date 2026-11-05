package wellbeing

import (
	"testing"
	"time"
)

var today = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

func fill(vals func(i int) (float64, bool)) map[string]float64 {
	m := map[string]float64{}
	for i := 0; i < 30; i++ {
		if v, ok := vals(i); ok {
			m[today.AddDate(0, 0, -i).Format("2006-01-02")] = v
		}
	}
	return m
}

func TestSlippingSleepIsDetectedAgainstOwnBaseline(t *testing.T) {
	s := Analyse("sleep", fill(func(i int) (float64, bool) {
		if i < 7 {
			return 5.5, true
		}
		return 7.5, true
	}), today, 0, 24)
	if s.Direction != "slipping" || s.Drop() < 0.2 {
		t.Fatalf("want slipping, got %s drop %.2f", s.Direction, s.Drop())
	}
	if s.Target == nil || *s.Target < 7 {
		t.Fatalf("target should be the person's good-day level, got %v", s.Target)
	}
	if len(s.Daily) != Window || !s.LoggedNow || s.Days7 != 7 {
		t.Fatalf("daily %d logged %v days %d", len(s.Daily), s.LoggedNow, s.Days7)
	}
}

func TestStressLowerIsBetter(t *testing.T) {
	s := Analyse("stress", fill(func(i int) (float64, bool) {
		if i < 7 {
			return 2, true
		}
		return 4, true
	}), today, 1, 5)
	if s.Direction != "improving" {
		t.Fatalf("falling stress should be improving, got %s", s.Direction)
	}
}

func TestTooLittleDataStaysUnknown(t *testing.T) {
	s := Analyse("sleep", fill(func(i int) (float64, bool) { return 7, i < 2 }), today, 0, 24)
	if s.Direction != "unknown" || s.Projected != nil || s.Target != nil {
		t.Fatalf("expected no claims from 2 days: %+v", s)
	}
}

func TestProjectionClamped(t *testing.T) {
	s := Analyse("mood", fill(func(i int) (float64, bool) { return float64(5 - i/6), true }), today, 1, 5)
	if s.Projected == nil || *s.Projected > 5 || *s.Projected < 1 {
		t.Fatalf("projection out of bounds: %v", s.Projected)
	}
}

func TestCombineRenormalises(t *testing.T) {
	if got := Combine([]Part{{"labs", 80, 40}, {"care", 60, 20}}); got != 73 {
		t.Fatalf("got %v", got)
	}
	if Combine(nil) != 0 {
		t.Fatal("empty should be 0")
	}
}

func TestHabitScoreNeedsData(t *testing.T) {
	if _, ok := HabitScore(nil); ok {
		t.Fatal("no data should give no score")
	}
}
