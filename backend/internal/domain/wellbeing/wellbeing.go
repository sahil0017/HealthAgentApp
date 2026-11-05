// Package wellbeing holds the pure maths behind the Summary dashboard: personal baselines, weekly projections and the score.
// Everything is measured against the person's own history; no population thresholds are used.
package wellbeing

import (
	"math"
	"sort"
	"time"
)

// Benefit says which direction is better for a habit: 1 higher, -1 lower, 0 neither (tracked but never coached).
var Benefit = map[string]int{"sleep": 1, "exercise": 1, "hydration": 1, "mood": 1, "stress": -1, "nutrition": 0}

const (
	Window       = 14 // days charted
	baselineDays = 30
	minBaseline  = 5
)

type Point struct {
	Day   string   `json:"d"`
	Value *float64 `json:"v"`
}

type Series struct {
	Code      string   `json:"code"`
	Days7     int      `json:"days_logged_7"`
	Avg7      *float64 `json:"avg_7"`
	Baseline  *float64 `json:"baseline"`
	Target    *float64 `json:"target"`
	Direction string   `json:"direction"` // improving, steady, slipping or unknown
	Projected *float64 `json:"projected_7d"`
	LoggedNow bool     `json:"logged_today"`
	Daily     []Point  `json:"daily"`
	Change    float64  `json:"-"`
	benefit   int
}

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func ptr(v float64) *float64 { r := math.Round(v*10) / 10; return &r }

// Analyse builds one habit's series from its daily values (keyed YYYY-MM-DD) as of today. lo and hi bound the projection.
func Analyse(code string, byDay map[string]float64, today time.Time, lo, hi float64) Series {
	s := Series{Code: code, Direction: "unknown", benefit: Benefit[code], Daily: make([]Point, 0, Window)}
	var week, older, recent []float64
	var xs, ys []float64
	for i := baselineDays - 1; i >= 0; i-- {
		key := today.AddDate(0, 0, -i).Format("2006-01-02")
		v, ok := byDay[key]
		if i < Window {
			p := Point{Day: key}
			if ok {
				p.Value = ptr(v)
			}
			s.Daily = append(s.Daily, p)
		}
		if !ok {
			continue
		}
		if i == 0 {
			s.LoggedNow = true
		}
		switch {
		case i < 7:
			week = append(week, v)
			s.Days7++
		default:
			older = append(older, v)
		}
		if i < Window {
			xs = append(xs, float64(Window-1-i))
			ys = append(ys, v)
		}
		recent = append(recent, v)
	}
	if len(week) > 0 {
		s.Avg7 = ptr(mean(week))
	}
	if len(older) >= minBaseline {
		s.Baseline = ptr(mean(older))
	}
	if s.benefit != 0 && len(recent) >= minBaseline {
		sorted := append([]float64(nil), recent...)
		sort.Float64s(sorted)
		q := sorted[(len(sorted)-1)*3/4]
		if s.benefit < 0 {
			q = sorted[(len(sorted)-1)/4]
		}
		s.Target = ptr(q)
	}
	if len(xs) >= minBaseline && s.Avg7 != nil {
		slope := slopeOf(xs, ys)
		proj := math.Max(lo, math.Min(hi, *s.Avg7+slope*7))
		s.Projected = ptr(proj)
		if s.Baseline != nil && *s.Baseline != 0 {
			s.Change = (*s.Avg7 - *s.Baseline) / math.Abs(*s.Baseline)
			better := s.Change * float64(s.benefit)
			switch {
			case s.benefit == 0 || math.Abs(s.Change) < 0.05:
				s.Direction = "steady"
			case better > 0:
				s.Direction = "improving"
			default:
				s.Direction = "slipping"
			}
		}
	}
	return s
}

// Drop is how far a slipping habit has fallen against the person's baseline, as a positive fraction.
func (s Series) Drop() float64 {
	if s.Direction != "slipping" {
		return 0
	}
	return math.Abs(s.Change)
}

func slopeOf(x, y []float64) float64 {
	mx, my := mean(x), mean(y)
	var num, den float64
	for i := range x {
		num += (x[i] - mx) * (y[i] - my)
		den += (x[i] - mx) * (x[i] - mx)
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// HabitScore is 0-100: how consistently the week was logged, and how close the week's average sat to the person's own good days.
func HabitScore(all []Series) (float64, bool) {
	var ratios []float64
	days := 0
	for _, s := range all {
		if s.benefit == 0 || s.Avg7 == nil || s.Target == nil || *s.Target == 0 {
			continue
		}
		r := *s.Avg7 / *s.Target
		if s.benefit < 0 {
			if *s.Avg7 == 0 {
				r = 1
			} else {
				r = *s.Target / *s.Avg7
			}
		}
		ratios = append(ratios, math.Min(1, r))
		days = max(days, s.Days7)
	}
	if len(ratios) == 0 {
		return 0, false
	}
	return 100 * (0.5*float64(days)/7 + 0.5*mean(ratios)), true
}

type Part struct {
	Key    string  `json:"key"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
}

// Combine weights the available parts and renormalises, so a missing area never drags the score down.
func Combine(parts []Part) float64 {
	var sum, w float64
	for _, p := range parts {
		sum += p.Score * p.Weight
		w += p.Weight
	}
	if w == 0 {
		return 0
	}
	return math.Round(sum / w)
}
