// Package lifestyle holds the pure rules for lifestyle logs and the symptom correlation maths.
package lifestyle

import (
	"math"
	"sort"
)

// Category describes one kind of daily lifestyle log.
type Category struct {
	Code  string  `json:"code"`
	Name  string  `json:"name"`
	Unit  string  `json:"unit"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Daily string  `json:"daily"` // how entries on one day combine: "sum" or "mean"
}

var categories = []Category{
	{"sleep", "Sleep", "h", 0, 24, "sum"},
	{"exercise", "Exercise", "min", 0, 1440, "sum"},
	{"nutrition", "Nutrition", "kcal", 0, 10000, "sum"},
	{"hydration", "Hydration", "ml", 0, 10000, "sum"},
	{"mood", "Mood (1 low - 5 high)", "score", 1, 5, "mean"},
	{"stress", "Stress (1 low - 5 high)", "score", 1, 5, "mean"},
}

func Categories() []Category { return append([]Category(nil), categories...) }

func Lookup(code string) (Category, bool) {
	for _, c := range categories {
		if c.Code == code {
			return c, true
		}
	}
	return Category{}, false
}

// ValidValue reports whether v is a finite number inside the category's range.
func (c Category) ValidValue(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= c.Min && v <= c.Max
}

// Combine folds one day's entries into a single number.
func (c Category) Combine(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	if c.Daily == "mean" && len(values) > 0 {
		return total / float64(len(values))
	}
	return total
}

// MinPairedDays is the fewest days with both series before a correlation is reported.
const MinPairedDays = 7

// Correlation is a Pearson coefficient with a plain-language strength.
type Correlation struct {
	R        float64 `json:"r"`
	Days     int     `json:"days"`
	Strength string  `json:"strength"`
}

// Pearson returns the coefficient for paired series, or false when there are too few pairs or no variance.
func Pearson(x, y []float64) (Correlation, bool) {
	n := len(x)
	if n != len(y) || n < MinPairedDays {
		return Correlation{}, false
	}
	var sx, sy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
	}
	mx, my := sx/float64(n), sy/float64(n)
	var sxy, sxx, syy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return Correlation{}, false
	}
	r := math.Round(sxy/math.Sqrt(sxx*syy)*100) / 100
	return Correlation{R: r, Days: n, Strength: strength(r)}, true
}

func strength(r float64) string {
	a := math.Abs(r)
	label := "negligible"
	switch {
	case a >= 0.7:
		label = "strong"
	case a >= 0.4:
		label = "moderate"
	case a >= 0.2:
		label = "weak"
	}
	if label == "negligible" {
		return label
	}
	if r < 0 {
		return label + " negative"
	}
	return label + " positive"
}

// SortedKeys returns the map's keys in order.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
