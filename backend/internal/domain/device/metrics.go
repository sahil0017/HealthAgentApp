// Package device holds the pure rules for readings from connected devices: which metrics are
// accepted, how units are normalised, which values are plausible and how readings are summarised.
// It has no database or HTTP code.
package device

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Range is an inclusive interval.
type Range struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

// Metric describes one measurement a device may report.
type Metric struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Group    string   `json:"group"`
	Unit     string   `json:"unit"`
	Sum      bool     `json:"cumulative"`
	LabCode  string   `json:"lab_biomarker_code,omitempty"`
	Typical  *Range   `json:"typical_adult_range,omitempty"`
	Accepted []string `json:"accepted_units"`

	min, max float64
	convert  map[string]func(float64) float64
}

var metrics = []Metric{
	{Key: "heart_rate", Name: "Heart rate", Group: "vitals", Unit: "bpm", min: 20, max: 300},
	{Key: "resting_heart_rate", Name: "Resting heart rate", Group: "vitals", Unit: "bpm", min: 20, max: 200, Typical: &Range{60, 100}},
	{Key: "hrv_sdnn", Name: "Heart rate variability (SDNN)", Group: "vitals", Unit: "ms", min: 1, max: 500},
	{Key: "blood_pressure_systolic", Name: "Systolic blood pressure", Group: "vitals", Unit: "mmHg", min: 50, max: 300, Typical: &Range{90, 120}},
	{Key: "blood_pressure_diastolic", Name: "Diastolic blood pressure", Group: "vitals", Unit: "mmHg", min: 30, max: 200, Typical: &Range{60, 80}},
	{Key: "oxygen_saturation", Name: "Blood oxygen saturation", Group: "vitals", Unit: "%", min: 50, max: 100, Typical: &Range{95, 100}},
	{Key: "respiratory_rate", Name: "Respiratory rate", Group: "vitals", Unit: "breaths/min", min: 4, max: 60, Typical: &Range{12, 20}},
	{Key: "body_temperature", Name: "Body temperature", Group: "vitals", Unit: "°C", min: 30, max: 45, Typical: &Range{36.1, 37.2},
		convert: map[string]func(float64) float64{"°F": func(v float64) float64 { return (v - 32) * 5 / 9 }}},
	{Key: "blood_glucose", Name: "Blood glucose", Group: "metabolic", Unit: "mg/dL", min: 20, max: 800, LabCode: "GLUCOSE",
		convert: map[string]func(float64) float64{"mmol/L": func(v float64) float64 { return v * 18.016 }}},
	{Key: "body_weight", Name: "Body weight", Group: "body", Unit: "kg", min: 2, max: 500,
		convert: map[string]func(float64) float64{"lb": func(v float64) float64 { return v * 0.45359237 }}},
	{Key: "steps", Name: "Steps", Group: "activity", Unit: "count", Sum: true, min: 0, max: 100000},
	{Key: "active_energy", Name: "Active energy", Group: "activity", Unit: "kcal", Sum: true, min: 0, max: 20000,
		convert: map[string]func(float64) float64{"kJ": func(v float64) float64 { return v / 4.184 }}},
	{Key: "sleep_duration", Name: "Sleep duration", Group: "activity", Unit: "h", Sum: true, min: 0, max: 24,
		convert: map[string]func(float64) float64{"min": func(v float64) float64 { return v / 60 }}},
	{Key: "vo2_max", Name: "Cardio fitness (VO2 max)", Group: "activity", Unit: "mL/(kg·min)", min: 5, max: 100},
}

var byKey = func() map[string]*Metric {
	out := map[string]*Metric{}
	for i := range metrics {
		m := &metrics[i]
		alt := []string{}
		for u := range m.convert {
			alt = append(alt, u)
		}
		sort.Strings(alt)
		m.Accepted = append([]string{m.Unit}, alt...)
		out[m.Key] = m
	}
	return out
}()

// Metrics returns every supported metric in a stable order.
func Metrics() []Metric { return metrics }

// Lookup finds a metric by key.
func Lookup(key string) (*Metric, bool) { m, ok := byKey[key]; return m, ok }

// Normalize converts a reading to the metric's canonical unit and checks it is physically plausible.
// The returned error is safe to show to the client.
func Normalize(metric string, value float64, unit string) (float64, error) {
	m, ok := byKey[metric]
	if !ok {
		return 0, fmt.Errorf("unsupported metric")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("value must be a finite number")
	}
	unit = strings.TrimSpace(unit)
	out := value
	switch {
	case unit == m.Unit:
	case m.convert[unit] != nil:
		out = m.convert[unit](value)
	default:
		return 0, fmt.Errorf("unit %q is not accepted for %s (use %s)", unit, metric, strings.Join(m.Accepted, ", "))
	}
	out = math.Round(out*1000) / 1000
	if out < m.min || out > m.max {
		return 0, fmt.Errorf("value is outside the plausible range for %s", metric)
	}
	return out, nil
}

// ToUnit converts a laboratory value in unit into the metric's canonical unit, reporting false when
// the unit is not one the metric understands.
func ToUnit(metric string, value float64, unit string) (float64, bool) {
	m, ok := byKey[metric]
	if !ok {
		return 0, false
	}
	unit = strings.TrimSpace(unit)
	if unit == m.Unit {
		return value, true
	}
	if f := m.convert[unit]; f != nil {
		return f(value), true
	}
	return 0, false
}
