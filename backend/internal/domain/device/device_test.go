package device

import (
	"testing"
	"time"
)

func TestNormalizeConvertsAndBoundsReadings(t *testing.T) {
	cases := []struct {
		metric string
		value  float64
		unit   string
		want   float64
		ok     bool
	}{
		{"heart_rate", 72, "bpm", 72, true},
		{"heart_rate", 500, "bpm", 0, false},
		{"heart_rate", 72, "mmHg", 0, false},
		{"body_temperature", 98.6, "°F", 37, true},
		{"body_weight", 154.3, "lb", 69.99, true},
		{"blood_glucose", 5.5, "mmol/L", 99.088, true},
		{"blood_glucose", 5, "mg/dL", 5, false},
		{"sleep_duration", 450, "min", 7.5, true},
		{"unknown_metric", 1, "x", 0, false},
	}
	for _, c := range cases {
		got, err := Normalize(c.metric, c.value, c.unit)
		if (err == nil) != c.ok || (c.ok && (got < c.want-0.01 || got > c.want+0.01)) {
			t.Errorf("%s %v %s: got %v, %v want %v ok=%v", c.metric, c.value, c.unit, got, err, c.want, c.ok)
		}
	}
}

func TestToUnitHandlesLabUnits(t *testing.T) {
	if v, ok := ToUnit("blood_glucose", 5, "mmol/L"); !ok || v < 90 || v > 90.2 {
		t.Fatalf("mmol/L conversion: %v %v", v, ok)
	}
	if _, ok := ToUnit("blood_glucose", 5, "g/dL"); ok {
		t.Fatal("unknown unit must not convert")
	}
}

func TestEveryMetricListsItsCanonicalUnitFirst(t *testing.T) {
	for _, m := range Metrics() {
		if len(m.Accepted) == 0 || m.Accepted[0] != m.Unit {
			t.Errorf("%s accepted units %v", m.Key, m.Accepted)
		}
	}
}

func TestSummarizeTrendWindowsAndTypicalRange(t *testing.T) {
	m, _ := Lookup("resting_heart_rate")
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	var days []Day
	for i := 0; i < 5; i++ {
		days = append(days, Day{Date: now.AddDate(0, 0, -i).Format("2006-01-02"), Mean: 80, Count: 3})
		days = append(days, Day{Date: now.AddDate(0, 0, -35-i).Format("2006-01-02"), Mean: 64, Count: 3})
	}
	s := Summarize(m, days, 80, now, "Watch", now)
	if s.Trend != "rising" || s.TrendPct == nil || *s.TrendPct < 24 || s.Days7 != 5 || s.OutsideRange {
		t.Fatalf("%+v", s)
	}

	few := Summarize(m, days[:2], 80, now, "", now)
	if few.Trend != "insufficient_data" {
		t.Fatalf("two days must not produce a trend: %+v", few)
	}

	high := []Day{{Date: now.Format("2006-01-02"), Mean: 130, Count: 1}}
	if !Summarize(m, high, 130, now, "", now).OutsideRange {
		t.Fatal("130 bpm average resting rate is outside the typical range")
	}
}

func TestCumulativeMetricsUseDailyTotals(t *testing.T) {
	m, _ := Lookup("steps")
	now := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	days := []Day{{Date: "2026-06-30", Mean: 100, Sum: 8000, Count: 80}, {Date: "2026-06-29", Mean: 100, Sum: 6000, Count: 60}}
	s := Summarize(m, days, 100, now, "", now)
	if s.Mean7 == nil || *s.Mean7 != 7000 {
		t.Fatalf("steps average must be of daily totals: %+v", s.Mean7)
	}
}
