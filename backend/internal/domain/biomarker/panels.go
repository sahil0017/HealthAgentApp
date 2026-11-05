package biomarker

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Category is a panel family. The catalog is learned, so each biomarker is assigned one when it is
// reviewed; the families themselves are fixed product concepts.
type Category struct {
	ID           string
	Label        string
	IntervalDays int // how often a person would normally repeat this panel
}

var Categories = []Category{
	{"cbc", "Complete blood count", 365},
	{"glucose", "Glucose and HbA1c", 180},
	{"lipid", "Lipid profile", 365},
	{"liver", "Liver markers", 365},
	{"kidney", "Kidney markers", 365},
	{"vitamin", "Vitamins and minerals", 365},
	{"hormone", "Hormones", 365},
	{"other", "Other measurements", 0},
}

func ValidCategory(id string) bool {
	for _, c := range Categories {
		if c.ID == id {
			return true
		}
	}
	return false
}

func CategoryOf(code string, categories map[string]string) string {
	if c, ok := categories[code]; ok {
		return c
	}
	return "other"
}

// CategoryLabel is the standard panel name shown to people.
func CategoryLabel(id string) string {
	for _, c := range Categories {
		if c.ID == id {
			return c.Label
		}
	}
	return "Other measurements"
}

// categoryWords maps words in a test's name to a panel, so tests the catalog has not classified still land in the
// right family. Checked in order: the first family with a matching word wins.
var categoryWords = []struct {
	id    string
	words []string
}{
	{"glucose", []string{"glucose", "hba1c", "a1c", "glycohemoglobin", "glycohaemoglobin", "glycated", "glycosylated", "insulin", "fructosamine"}},
	{"lipid", []string{"cholesterol", "ldl", "hdl", "vldl", "triglyceride", "triglycerides", "lipoprotein", "lipid"}},
	{"liver", []string{"bilirubin", "sgot", "sgpt", "alt", "ast", "alanine", "aspartate", "alkaline", "phosphatase", "ggt", "gamma", "albumin", "globulin", "transaminase", "transferase"}},
	{"kidney", []string{"creatinine", "urea", "bun", "uric", "urate", "egfr", "cystatin"}},
	{"hormone", []string{"tsh", "thyroid", "thyroxine", "triiodothyronine", "t3", "t4", "cortisol", "testosterone", "estradiol", "prolactin", "fsh", "lh", "progesterone", "dhea", "hormone"}},
	{"vitamin", []string{"vitamin", "b12", "cobalamin", "folate", "folic", "ferritin", "iron", "tibc", "transferrin", "calcium", "magnesium", "zinc", "phosphorus", "sodium", "potassium", "chloride"}},
	{"cbc", []string{"hemoglobin", "haemoglobin", "hematocrit", "haematocrit", "pcv", "rbc", "wbc", "platelet", "platelets", "leukocyte", "leucocyte", "neutrophil", "neutrophils", "lymphocyte", "lymphocytes", "monocyte", "monocytes", "eosinophil", "eosinophils", "basophil", "basophils", "mcv", "mch", "mchc", "rdw", "mpv", "corpuscular", "blood", "cell", "cells"}},
}

// CategoryFor uses the catalog's panel when it has one and otherwise reads the test's name.
func CategoryFor(code, name string, categories map[string]string) string {
	if c, ok := categories[code]; ok && c != "other" {
		return c
	}
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(code+" "+name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		words[w] = true
	}
	for _, f := range categoryWords {
		for _, w := range f.words {
			if words[w] {
				return f.id
			}
		}
	}
	return "other"
}

// SourceConfidence says how far a reading can be trusted as a record of a measurement: a reviewed
// laboratory value is strongest, a reviewed manual or device value is weaker, anything unreviewed is low.
func SourceConfidence(status, sourceType string) string {
	switch {
	case status != "VERIFIED":
		return "low"
	case MeasurementKind(sourceType) == "lab":
		return "high"
	}
	return "medium"
}

// Change compares one biomarker at two dates.
type Change struct {
	BiomarkerCode string       `json:"biomarker_code"`
	DisplayName   string       `json:"display_name"`
	Category      string       `json:"category"`
	Unit          string       `json:"unit"`
	From          *ChangePoint `json:"from"`
	To            *ChangePoint `json:"to"`
	Change        *float64     `json:"change"`
	ChangePercent *float64     `json:"change_percent"`
	Status        string       `json:"status"`
}

// ChangePoint is a value and the date it was measured.
type ChangePoint struct {
	Value float64 `json:"value"`
	On    string  `json:"observed_at"`
	Flag  string  `json:"flag"`
}

// ReadingAtOrBefore returns the latest verified point on or before the date.
func ReadingAtOrBefore(points []TrendPoint, day string) *ChangePoint {
	var best *TrendPoint
	for i := range points {
		p := &points[i]
		if p.Verification == "VERIFIED" && p.ObservedAt <= day && (best == nil || p.ObservedAt >= best.ObservedAt) {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	return &ChangePoint{Value: best.Value, On: best.ObservedAt, Flag: best.Flag}
}

// Compare lines a biomarker up at two dates. Values are already in one unit because they come from
// a converted trend, so the change is meaningful across differently-reported tests.
func Compare(t Trend, category, from, to string) Change {
	c := Change{BiomarkerCode: t.BiomarkerCode, DisplayName: t.DisplayName, Category: category, Unit: t.CanonicalUnit}
	c.From, c.To = ReadingAtOrBefore(t.Points, from), ReadingAtOrBefore(t.Points, to)
	switch {
	case c.From == nil && c.To == nil:
		c.Status = "no_data"
	case c.From == nil:
		c.Status = "new"
	case c.To == nil:
		c.Status = "no_recent_data"
	case c.From.On == c.To.On:
		c.Status = "same_reading"
	default:
		d := c.To.Value - c.From.Value
		c.Change = &d
		if c.From.Value != 0 {
			pct := math.Round(d/math.Abs(c.From.Value)*1000) / 10
			c.ChangePercent = &pct
		}
		switch {
		case outOfRange(c.From.Flag) && c.To.Flag == "within_range":
			c.Status = "returned_to_range"
		case c.From.Flag == "within_range" && outOfRange(c.To.Flag):
			c.Status = "left_range"
		case d == 0:
			c.Status = "unchanged"
		default:
			c.Status = "changed"
		}
	}
	return c
}

// PanelCoverage reports, per category, whether the person has recent verified measurements.
type PanelCoverage struct {
	Category      string   `json:"category"`
	Label         string   `json:"label"`
	Biomarkers    []string `json:"biomarkers"`
	LastMeasured  *string  `json:"last_measured"`
	DaysSinceLast *int     `json:"days_since_last"`
	IntervalDays  int      `json:"interval_days"`
	Status        string   `json:"status"` // current, stale, never_measured
}

// Coverage detects missing data: panels never measured and panels older than their usual interval.
// "other" is listed only when it holds something, since it has no expected cadence.
func Coverage(trends map[string]Trend, categories map[string]string, now time.Time) []PanelCoverage {
	byCat := map[string]*PanelCoverage{}
	for _, c := range Categories {
		byCat[c.ID] = &PanelCoverage{Category: c.ID, Label: c.Label, Biomarkers: []string{}, IntervalDays: c.IntervalDays, Status: "never_measured"}
	}
	codes := make([]string, 0, len(trends))
	for code := range trends {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		t := trends[code]
		var last *string
		for i := range t.Points {
			if t.Points[i].Verification == "VERIFIED" && (last == nil || t.Points[i].ObservedAt > *last) {
				d := t.Points[i].ObservedAt
				last = &d
			}
		}
		if last == nil {
			continue
		}
		pc := byCat[CategoryOf(code, categories)]
		pc.Biomarkers = append(pc.Biomarkers, t.DisplayName)
		if pc.LastMeasured == nil || *last > *pc.LastMeasured {
			pc.LastMeasured = last
		}
	}
	out := []PanelCoverage{}
	for _, c := range Categories {
		pc := byCat[c.ID]
		if pc.LastMeasured != nil {
			if day, err := time.Parse("2006-01-02", *pc.LastMeasured); err == nil {
				since := int(now.Sub(day).Hours() / 24)
				pc.DaysSinceLast = &since
				pc.Status = "current"
				if pc.IntervalDays > 0 && since > pc.IntervalDays {
					pc.Status = "stale"
				}
			}
		} else if c.ID == "other" {
			continue
		}
		out = append(out, *pc)
	}
	return out
}

func outOfRange(flag string) bool { return flag == "below_range" || flag == "above_range" }
