package api

import (
	"math"
	"testing"
)

func intp(v int) *int       { return &v }
func strp(v string) *string { return &v }

func TestParseReferenceRange(t *testing.T) {
	cases := []struct {
		in      string
		low, hi *float64
		ok      bool
	}{
		{"70-99", f(70), f(99), true},
		{" 70 – 99 mg/dL ", f(70), f(99), true},
		{"3.5 to 5.0", f(3.5), f(5), true},
		{"< 5.7", nil, f(5.7), true},
		{"≤5.7", nil, f(5.7), true},
		{">= 40", f(40), nil, true},
		{"> 1,000", f(1000), nil, true},
		{"(0.5-1.2)", f(0.5), f(1.2), true},
		{"99-70", nil, nil, false},
		{"negative", nil, nil, false},
		{"", nil, nil, false},
	}
	for _, c := range cases {
		low, hi, ok := parseReferenceRange(c.in)
		if ok != c.ok || (low == nil) != (c.low == nil) || (hi == nil) != (c.hi == nil) ||
			(low != nil && *low != *c.low) || (hi != nil && *hi != *c.hi) {
			t.Errorf("%q: got %v %v %v", c.in, low, hi, ok)
		}
	}
}

func TestFlagValue(t *testing.T) {
	if flagValue(5, f(1), f(10)) != "within_range" || flagValue(1, f(1), f(10)) != "within_range" || flagValue(10, f(1), f(10)) != "within_range" {
		t.Error("bounds are inclusive")
	}
	if flagValue(0.9, f(1), f(10)) != "below_range" || flagValue(10.1, f(1), f(10)) != "above_range" {
		t.Error("outside the range")
	}
	if flagValue(11, nil, f(10)) != "above_range" || flagValue(11, f(1), nil) != "within_range" || flagValue(1, nil, nil) != "unknown" {
		t.Error("one-sided and missing ranges")
	}
}

func TestConvertValue(t *testing.T) {
	known := map[conversionKey]conversion{{"a", "b"}: {Factor: 2, Offset: 10}}
	if v, ok := convertValue(5, "a", "b", known); !ok || v != 20 {
		t.Errorf("forward: %v %v", v, ok)
	}
	if v, ok := convertValue(20, "b", "a", known); !ok || math.Abs(v-5) > 1e-9 {
		t.Errorf("reverse must invert the offset too: %v %v", v, ok)
	}
	if v, ok := convertValue(3, "a", "a", nil); !ok || v != 3 {
		t.Error("same unit needs no conversion")
	}
	if _, ok := convertValue(1, "a", "c", known); ok {
		t.Error("an unknown pair must not be guessed")
	}
	chain := map[conversionKey]conversion{{"a", "b"}: {Factor: 2}, {"b", "c"}: {Factor: 3}}
	if _, ok := convertValue(1, "a", "c", chain); ok {
		t.Error("conversions must not be chained")
	}
}

func TestPickLearnedRange(t *testing.T) {
	general := learnedRange{Unit: "u", Low: f(1), High: f(9), Source: "general"}
	female := learnedRange{Unit: "u", Sex: strp("female"), Low: f(2), High: f(8), Source: "female"}
	adultFemale := learnedRange{Unit: "u", Sex: strp("female"), AgeMin: intp(18), AgeMax: intp(65), Low: f(3), High: f(7), Source: "adult female"}
	child := learnedRange{Unit: "u", AgeMax: intp(12), Low: f(0), High: f(4), Source: "child"}
	all := []learnedRange{general, female, adultFemale, child}

	if got := pickLearnedRange(all, strp("female"), intp(30)); got == nil || got.Source != "adult female" {
		t.Errorf("most specific wins: %+v", got)
	}
	if got := pickLearnedRange(all, strp("female"), intp(70)); got == nil || got.Source != "female" {
		t.Errorf("age outside the band falls back: %+v", got)
	}
	if got := pickLearnedRange(all, nil, nil); got == nil || got.Source != "general" {
		t.Errorf("unknown sex and age must only use the general range: %+v", got)
	}
	if got := pickLearnedRange(all, strp("male"), intp(8)); got == nil || got.Source != "child" {
		t.Errorf("child range: %+v", got)
	}
	if got := pickLearnedRange([]learnedRange{female}, strp("male"), intp(30)); got != nil {
		t.Errorf("another group's range must not apply: %+v", got)
	}
}

func TestAnalyzeTrend(t *testing.T) {
	pts := func(vals ...float64) []point {
		out := []point{}
		for n, v := range vals {
			out = append(out, point{Value: v, On: day("2026-01-01").AddDate(0, n, 0)})
		}
		return out
	}
	if got := analyzeTrend(pts(5)); got.Direction != "insufficient_data" {
		t.Errorf("one point: %+v", got)
	}
	if got := analyzeTrend([]point{{1, day("2026-01-01")}, {9, day("2026-01-01")}}); got.Direction != "insufficient_data" {
		t.Errorf("same date cannot define a trend: %+v", got)
	}
	if got := analyzeTrend(pts(5, 6, 7, 8)); got.Direction != "rising" || got.ChangePerYear == nil || *got.ChangePerYear <= 0 {
		t.Errorf("rising: %+v", got)
	}
	if got := analyzeTrend(pts(8, 7, 6, 5)); got.Direction != "falling" {
		t.Errorf("falling: %+v", got)
	}
	if got := analyzeTrend(pts(100, 101, 99, 100)); got.Direction != "stable" {
		t.Errorf("small movement is stable: %+v", got)
	}
	shuffled := []point{{8, day("2026-04-01")}, {5, day("2026-01-01")}, {6, day("2026-02-01")}}
	if got := analyzeTrend(shuffled); got.Direction != "rising" || got.SpanDays != 90 {
		t.Errorf("order must not matter: %+v", got)
	}
}
