// Package biomarker is the pure clinical-data logic for measurements: unit conversion, reference
// ranges, trend analysis and measurement cadence. It has no database or HTTP code, so it is safe to
// call from any module and cheap to unit-test.
package biomarker

import (
	"math"
	"sort"
	"time"

	"cortex/backend/internal/labrange"
)

// StableBand is the share of the average value within which a change over the whole period is
// described as "stable". It is a descriptive threshold, not a clinical one.
const StableBand = 0.05

// ParseReferenceRange reads the range printed on a lab report; see labrange.Parse.
func ParseReferenceRange(text string) (low, high *float64, ok bool) { return labrange.Parse(text) }

// FlagValue compares a value with a range. It only ever says where the value sits relative to
// the range; it never interprets what that means.
func FlagValue(value float64, low, high *float64) string {
	switch {
	case low == nil && high == nil:
		return "unknown"
	case low != nil && value < *low:
		return "below_range"
	case high != nil && value > *high:
		return "above_range"
	}
	return "within_range"
}

type Conversion struct{ Factor, Offset float64 }

type ConversionKey struct{ From, To string }

// ConvertValue converts using an approved conversion, in either direction. It never chains
// conversions and never invents one: an unknown pair returns ok=false.
func ConvertValue(value float64, from, to string, known map[ConversionKey]Conversion) (float64, bool) {
	if from == to {
		return value, true
	}
	if c, found := known[ConversionKey{from, to}]; found {
		return value*c.Factor + c.Offset, true
	}
	if c, found := known[ConversionKey{to, from}]; found && c.Factor != 0 {
		return (value - c.Offset) / c.Factor, true
	}
	return 0, false
}

type LearnedRange struct {
	Unit   string
	Sex    *string
	AgeMin *int
	AgeMax *int
	Low    *float64
	High   *float64
	Source string
}

func (r LearnedRange) specificity() int {
	n := 0
	if r.Sex != nil {
		n++
	}
	if r.AgeMin != nil || r.AgeMax != nil {
		n++
	}
	return n
}

// PickLearnedRange returns the most specific approved range that applies to this person. A range
// limited by sex or age is skipped when that detail is unknown, so the wrong group's range is
// never applied by default.
func PickLearnedRange(candidates []LearnedRange, sex *string, age *int) *LearnedRange {
	var best *LearnedRange
	for i := range candidates {
		c := candidates[i]
		if c.Sex != nil && (sex == nil || *sex != *c.Sex) {
			continue
		}
		if c.AgeMin != nil || c.AgeMax != nil {
			if age == nil || (c.AgeMin != nil && *age < *c.AgeMin) || (c.AgeMax != nil && *age > *c.AgeMax) {
				continue
			}
		}
		if best == nil || c.specificity() > best.specificity() {
			best = &candidates[i]
		}
	}
	return best
}

type Point struct {
	Value float64
	On    time.Time
}

type TrendResult struct {
	Direction     string   `json:"direction"`
	Points        int      `json:"points"`
	SpanDays      int      `json:"span_days"`
	ChangePerYear *float64 `json:"change_per_year"`
}

// AnalyzeTrend describes the direction of a series using a least-squares line over the dates.
// It needs at least two different dates; with fewer it says so rather than guessing.
func AnalyzeTrend(points []Point) TrendResult {
	out := TrendResult{Direction: "insufficient_data", Points: len(points)}
	if len(points) < 2 {
		return out
	}
	sorted := append([]Point(nil), points...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].On.Before(sorted[j].On) })
	origin := sorted[0].On
	span := sorted[len(sorted)-1].On.Sub(origin).Hours() / 24
	out.SpanDays = int(span + 0.5)
	if span <= 0 {
		return out
	}
	var sumX, sumY, sumXY, sumXX, sumAbs float64
	n := float64(len(sorted))
	for _, p := range sorted {
		x := p.On.Sub(origin).Hours() / 24
		sumX += x
		sumY += p.Value
		sumXY += x * p.Value
		sumXX += x * x
		sumAbs += math.Abs(p.Value)
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return out
	}
	slope := (n*sumXY - sumX*sumY) / denom
	perYear := slope * 365
	out.ChangePerYear = &perYear
	total := slope * span
	mean := sumAbs / n
	switch {
	case math.Abs(total) <= StableBand*mean:
		out.Direction = "stable"
	case total > 0:
		out.Direction = "rising"
	default:
		out.Direction = "falling"
	}
	return out
}
