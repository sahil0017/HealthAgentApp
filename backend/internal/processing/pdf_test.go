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
	got, err := (PDFTextProcessor{}).Process(context.Background(), "image/png", []byte("image bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.ManualReview || len(got.Candidates) != 0 {
		t.Fatalf("image result = %#v", got)
	}
}
