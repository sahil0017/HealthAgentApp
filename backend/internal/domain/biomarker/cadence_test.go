package biomarker

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

func TestMeasurementCadenceUsesTheMedianGapOfDistinctDays(t *testing.T) {
	if _, ok := MeasurementCadence([]time.Time{day("2026-01-01"), day("2026-02-01")}); ok {
		t.Fatal("two dates are not enough to learn a cadence")
	}
	if _, ok := MeasurementCadence([]time.Time{day("2026-01-01"), day("2026-01-01"), day("2026-01-01")}); ok {
		t.Fatal("repeated readings on one day are a single date")
	}
	got, ok := MeasurementCadence([]time.Time{day("2026-01-01"), day("2026-01-31"), day("2026-03-02"), day("2026-06-30")})
	if !ok || got != 30*24*time.Hour {
		t.Fatalf("median of 30,30,120 days should be 30 days, got %v %v", got, ok)
	}
	even, _ := MeasurementCadence([]time.Time{day("2026-01-01"), day("2026-01-11"), day("2026-02-10")})
	if even != 20*24*time.Hour {
		t.Fatalf("median of 10,30 days should be 20 days, got %v", even)
	}
}

func TestOverdueForMeasurementNeedsMoreThanTwiceTheUsualGap(t *testing.T) {
	cadence := 30 * 24 * time.Hour
	last := day("2026-01-01")
	if OverdueForMeasurement(last, day("2026-03-02"), cadence) {
		t.Error("60 days is exactly twice the gap and not yet overdue")
	}
	if !OverdueForMeasurement(last, day("2026-03-03"), cadence) {
		t.Error("61 days should be overdue")
	}
	if OverdueForMeasurement(last, day("2030-01-01"), 0) {
		t.Error("without a cadence nothing is overdue")
	}
}
