package biomarkers

import (
	"testing"

	"cortex/backend/internal/domain/biomarker"
)

func TestPreviousReportPoint(t *testing.T) {
	points := []biomarker.TrendPoint{
		{DocumentID: "older", MeasurementKind: "lab", ObservedAt: "2024-01-01", Value: 4},
		{DocumentID: "current", MeasurementKind: "lab", ObservedAt: "2024-02-01", Value: 7},
		{DocumentID: "", MeasurementKind: "device", ObservedAt: "2024-02-02", Value: 99},
		{DocumentID: "same-day", MeasurementKind: "lab", ObservedAt: "2024-02-03", Value: 10},
		{DocumentID: "current", MeasurementKind: "lab", ObservedAt: "2024-02-03", Value: 12},
	}
	got, ok := previousReportPoint(points, 4)
	if !ok || got.DocumentID != "older" || got.Value != 4 {
		t.Fatalf("got %+v, %v; want older report", got, ok)
	}
	points[0].DateInferred = true
	if _, ok := previousReportPoint(points, 4); ok {
		t.Fatal("must not compare inferred dates")
	}
	points[4].DateInferred = true
	points[0].DateInferred = false
	if _, ok := previousReportPoint(points, 4); ok {
		t.Fatal("must not compare a current reading with an inferred date")
	}
	ambiguous := []biomarker.TrendPoint{
		{DocumentID: "older-a", MeasurementKind: "lab", ObservedAt: "2024-01-01", Value: 4},
		{DocumentID: "older-b", MeasurementKind: "lab", ObservedAt: "2024-01-01", Value: 5},
		{DocumentID: "current", MeasurementKind: "lab", ObservedAt: "2024-02-01", Value: 8},
	}
	if _, ok := previousReportPoint(ambiguous, 2); ok {
		t.Fatal("must not guess between different earlier readings on the same day")
	}
}
