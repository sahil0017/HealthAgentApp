package biomarker

import (
	"testing"
	"time"
)

func pt(on string, v float64, flag, status string) TrendPoint {
	return TrendPoint{ObservedAt: on, Value: v, Flag: flag, Verification: status}
}

func TestSourceConfidence(t *testing.T) {
	for _, c := range []struct{ status, source, want string }{
		{"VERIFIED", "LAB_DOCUMENT", "high"}, {"VERIFIED", "MANUAL", "medium"},
		{"VERIFIED", "DEVICE_APPLE_HEALTH", "medium"}, {"PENDING", "LAB_DOCUMENT", "low"},
	} {
		if got := SourceConfidence(c.status, c.source); got != c.want {
			t.Errorf("%s/%s = %s, want %s", c.status, c.source, got, c.want)
		}
	}
}

func TestCompareClassifiesChangesAndIgnoresUnverified(t *testing.T) {
	tr := Trend{BiomarkerCode: "GLUCOSE", DisplayName: "Glucose", CanonicalUnit: "mg/dL", Points: []TrendPoint{
		pt("2025-01-10", 130, "above_range", "VERIFIED"), pt("2026-01-10", 95, "within_range", "VERIFIED"), pt("2026-02-01", 300, "above_range", "PENDING"),
	}}
	c := Compare(tr, "glucose", "2025-06-01", "2026-03-01")
	if c.Status != "returned_to_range" || c.To.Value != 95 || *c.Change != -35 {
		t.Fatalf("%+v", c)
	}
	if got := Compare(tr, "glucose", "2024-01-01", "2026-03-01").Status; got != "new" {
		t.Errorf("no earlier reading should be new, got %s", got)
	}
	if got := Compare(tr, "glucose", "2024-01-01", "2024-06-01").Status; got != "no_data" {
		t.Errorf("got %s", got)
	}
	if got := Compare(tr, "glucose", "2026-01-20", "2026-03-01").Status; got != "same_reading" {
		t.Errorf("got %s", got)
	}
}

func TestCoverageFlagsNeverMeasuredAndStalePanels(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	trends := map[string]Trend{
		"HBA1C": {DisplayName: "HbA1c", Points: []TrendPoint{pt("2025-11-01", 5.5, "within_range", "VERIFIED")}},
		"LDL":   {DisplayName: "LDL", Points: []TrendPoint{pt("2026-05-01", 100, "within_range", "VERIFIED")}},
		"ODD":   {DisplayName: "Odd", Points: []TrendPoint{pt("2026-05-01", 1, "unknown", "PENDING")}},
	}
	got := map[string]PanelCoverage{}
	for _, c := range Coverage(trends, map[string]string{"HBA1C": "glucose", "LDL": "lipid"}, now) {
		got[c.Category] = c
	}
	if got["glucose"].Status != "stale" || got["lipid"].Status != "current" || got["cbc"].Status != "never_measured" {
		t.Fatalf("%+v", got)
	}
	if _, ok := got["other"]; ok {
		t.Error("unverified readings must not create an 'other' panel")
	}
}

func TestCategoryForReadsTheNameWhenTheCatalogHasNoPanel(t *testing.T) {
	cases := map[string]string{
		"Very Low-Density Lipoprotein (VLDL)": "lipid",
		"Bilirubin, Direct":                   "liver",
		"Gamma-glutamyl Transferase":          "liver",
		"Mean Corpuscular Hemoglobin":         "cbc",
		"Absolute Basophil Count (ABC)":       "cbc",
		"Glycated Hemoglobin":                 "glucose",
		"Uric Acid":                           "kidney",
		"Vitamin B12":                         "vitamin",
		"Thyroid Stimulating Hormone":         "hormone",
		"Something Unrecognised":              "other",
	}
	for name, want := range cases {
		if got := CategoryFor("X", name, nil); got != want {
			t.Errorf("%s: got %s want %s", name, got, want)
		}
	}
	if CategoryFor("X", "Bilirubin", map[string]string{"X": "kidney"}) != "kidney" {
		t.Error("the catalog's panel must win over the name")
	}
	if CategoryLabel("lipid") != "Lipid profile" {
		t.Error("label")
	}
}
