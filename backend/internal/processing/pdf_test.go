package processing

import (
	"context"
	"strings"
	"testing"

	"github.com/giraffesyo/pdf/pdftest"
)

var testCatalog = []CatalogEntry{
	{Code: "HBA1C", Name: "Hemoglobin A1c", Terms: []string{"HbA1c", "A1C"}, Units: []string{"%"}},
	{Code: "GLUCOSE", Name: "Glucose", Units: []string{"mg/dL"}},
}

func TestParseCandidatesUsesCatalogAndReportsUnknownLabels(t *testing.T) {
	got, unknown := ParseCandidates("HbA1c 6.2 %\nFasting Glucose: 98 mg/dL\nFerritin 45 ng/mL\nLDL < 30 mg/dL\nPage 2 of 3\n", 2, testCatalog)
	if len(got) != 3 {
		t.Fatalf("candidate count = %d, want 3: %#v", len(got), got)
	}
	if got[0].BiomarkerCode != "HBA1C" || got[0].Value != 6.2 || got[0].Unit != "%" || got[0].Page != 2 {
		t.Fatalf("unexpected HbA1c candidate: %#v", got[0])
	}
	if got[1].BiomarkerCode != "GLUCOSE" || got[1].Value != 98 {
		t.Fatalf("unexpected glucose candidate: %#v", got[1])
	}
	if got[2].BiomarkerCode != "" || len(unknown) != 1 || unknown[0] != "Ferritin" {
		t.Fatalf("unknown handling wrong: %#v %v", got[2], unknown)
	}
}

func TestParseCandidatesWithEmptyCatalogAssignsNoCodes(t *testing.T) {
	got, _ := ParseCandidates("HbA1c 6.2 %\n", 1, nil)
	if len(got) != 1 || got[0].BiomarkerCode != "" {
		t.Fatalf("got %#v", got)
	}
}

func TestPDFTextProcessorExtractsTextAndMarksCandidatesUnverified(t *testing.T) {
	data := pdftest.Build(1,
		pdftest.Catalog(2),
		pdftest.Pages(3),
		pdftest.Page(2, 4, "<< /Font << /F1 5 0 R >> >>"),
		pdftest.Stream("", "BT /F1 12 Tf 72 720 Td (HbA1c 6.2 %) Tj ET"),
		pdftest.Helvetica(),
	)
	got, err := (PDFTextProcessor{Catalog: func(context.Context) []CatalogEntry { return testCatalog }}).Process(context.Background(), "application/pdf", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "HbA1c 6.2 %") {
		t.Fatalf("extracted text = %q", got.Text)
	}
	if got.ManualReview {
		t.Fatal("text PDF should be queued for value review, not marked textless")
	}
	if len(got.Candidates) != 1 || got.Candidates[0].BiomarkerCode != "HBA1C" {
		t.Fatalf("candidates = %#v", got.Candidates)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected an unverified-candidate warning")
	}
}

func TestPDFTextProcessorMarksImagesForManualReview(t *testing.T) {
	old := ocrBinary
	ocrBinary = "definitely-not-installed-ocr"
	t.Cleanup(func() { ocrBinary = old })
	got, err := (PDFTextProcessor{}).Process(context.Background(), "image/png", []byte("image bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.ManualReview || len(got.Candidates) != 0 {
		t.Fatalf("image result = %#v", got)
	}
}

func TestParseCandidatesCapturesReferenceRangeAndReportDate(t *testing.T) {
	catalog := []CatalogEntry{{Code: "GLUCOSE", Name: "Glucose", Terms: []string{"glucose"}}, {Code: "LDL", Name: "LDL", Terms: []string{"ldl"}}}
	text := "Date of birth: 1980-02-03\nCollected: 2026-10-15\nGlucose 104 mg/dL 70 - 99\nLDL 131 mg/dL (<100)\nFerritin 45 ng/mL"
	got, _ := ParseCandidates(text, 1, catalog)
	if len(got) != 3 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	if got[0].ReferenceRange != "70 - 99" || got[1].ReferenceRange != "<100" || got[2].ReferenceRange != "" {
		t.Fatalf("ranges: %q %q %q", got[0].ReferenceRange, got[1].ReferenceRange, got[2].ReferenceRange)
	}
	for _, c := range got {
		if c.ObservedAt == nil || *c.ObservedAt != "2026-10-15" {
			t.Fatalf("observed_at should be the collection date, got %v", c.ObservedAt)
		}
	}
}

func TestReportDateIgnoresUnlabelledAndImpossibleDates(t *testing.T) {
	for _, in := range []string{"Date of birth: 1980-02-03", "Collected: 2026-02-31", "Collected: 2999-01-01", "no dates"} {
		if d := ReportDate(in); d != nil {
			t.Errorf("%q should yield no date, got %s", in, *d)
		}
	}
	if d := ReportDate("Report date: March 4, 2026"); d == nil || *d != "2026-03-04" {
		t.Fatalf("month-name date not parsed: %v", d)
	}
}

func TestParseCandidatesReadsTableRowsWithUnitOnNextLine(t *testing.T) {
	cat := append([]CatalogEntry{}, testCatalog...)
	cat = append(cat, CatalogEntry{Code: "LDL", Name: "LDL Cholesterol", Terms: []string{"Low-Density Lipoprotein (LDL) Cholesterol"}, Units: []string{"mg/dL"}})
	text := "Collected : 14/09/2026 10:19 AM\nTest Result Biological Reference Interval\nLow-Density Lipoprotein (LDL) 86 < 100\nCholesterol mg/dL\nCalculated\nGlucose 98 70 - 99\nHexokinase mg/dL\nPlatelet Hematocrit 0.315 0.2 - 0.5\nCalculated %\n"
	got, _ := ParseCandidates(text, 1, cat)
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %+v", got)
	}
	if got[0].BiomarkerCode != "LDL" || got[0].Value != 86 || got[0].Unit != "mg/dL" || got[0].ReferenceRange != "< 100" {
		t.Fatalf("wrapped LDL row wrong: %+v", got[0])
	}
	if got[1].BiomarkerCode != "GLUCOSE" || got[1].ReferenceRange != "70 - 99" {
		t.Fatalf("glucose row wrong: %+v", got[1])
	}
	if got[0].ObservedAt == nil || *got[0].ObservedAt != "2026-09-14" {
		t.Fatalf("day-first date not read: %v", got[0].ObservedAt)
	}
}

func TestRangeWordingIsNotATest(t *testing.T) {
	for _, l := range []string{"More than", "Less than", "Optimal", "Desirable", "Up to", "Above"} {
		if !IsRangeText(l) {
			t.Errorf("%q should be range wording", l)
		}
	}
	for _, l := range []string{"High Sensitivity CRP", "Low Density Lipoprotein", "Normal Cells", "Total Cholesterol", "Higher Glucose"} {
		if IsRangeText(l) {
			t.Errorf("%q is a test name", l)
		}
	}
}
