package processing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/giraffesyo/pdf/pdftest"
)

func fakeTesseract(t *testing.T, output string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-tesseract")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + output + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := ocrBinary
	ocrBinary = script
	t.Cleanup(func() { ocrBinary = old })
}

// fakeScannedPDFTools makes tesseract echo its input and pdftoppm write one "image" per page text.
func fakeScannedPDFTools(t *testing.T, pageTexts ...string) {
	t.Helper()
	dir := t.TempDir()
	tess := filepath.Join(dir, "tess")
	if err := os.WriteFile(tess, []byte("#!/bin/sh\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor last; do :; done\n"
	for i, text := range pageTexts {
		script += "printf '%s' '" + text + "' > \"$last-0" + string(rune('1'+i)) + ".png\"\n"
	}
	raster := filepath.Join(dir, "raster")
	if err := os.WriteFile(raster, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldO, oldR := ocrBinary, rasterBinary
	ocrBinary, rasterBinary = tess, raster
	t.Cleanup(func() { ocrBinary, rasterBinary = oldO, oldR })
}

func blankPDF() []byte {
	return pdftest.Build(1,
		pdftest.Catalog(2),
		pdftest.Pages(3),
		pdftest.Page(2, 4, "<< /Font << /F1 5 0 R >> >>"),
		pdftest.Stream("", ""),
		pdftest.Helvetica(),
	)
}

func TestScannedPDFIsOCRdPageByPage(t *testing.T) {
	fakeScannedPDFTools(t, "HbA1c 6.2 %", "Ferritin 45 ng/mL")
	got, err := PDFTextProcessor{Catalog: func(context.Context) []CatalogEntry { return testCatalog }}.Process(context.Background(), "application/pdf", blankPDF())
	if err != nil {
		t.Fatal(err)
	}
	if got.ManualReview || len(got.Pages) != 2 {
		t.Fatalf("want 2 OCR'd pages, got %#v", got)
	}
	if len(got.Candidates) != 2 || got.Candidates[0].Page != 1 || got.Candidates[1].Page != 2 {
		t.Fatalf("candidates must carry their page: %#v", got.Candidates)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("OCR output must be flagged as unverified")
	}
}

func TestScannedPDFWithoutRasterToolNeedsManualReview(t *testing.T) {
	oldR := rasterBinary
	rasterBinary = "definitely-not-installed-raster"
	t.Cleanup(func() { rasterBinary = oldR })
	got, err := PDFTextProcessor{}.Process(context.Background(), "application/pdf", blankPDF())
	if err != nil || !got.ManualReview {
		t.Fatalf("want manual review, got %#v err=%v", got, err)
	}
}

func TestOCRLanguagesAreValidated(t *testing.T) {
	for in, want := range map[string]string{"": "eng", "eng+hin": "eng+hin", "deu": "deu", "eng; rm -rf /": "eng", "../x": "eng", "EN": "eng"} {
		t.Setenv("CORTEX_OCR_LANGS", in)
		if got := ocrLanguages(); got != want {
			t.Errorf("ocrLanguages(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageWithoutOCRFallsBackToManualReview(t *testing.T) {
	old := ocrBinary
	ocrBinary = "definitely-not-installed-ocr"
	t.Cleanup(func() { ocrBinary = old })
	got, err := PDFTextProcessor{}.Process(context.Background(), "image/png", []byte("img"))
	if err != nil || !got.ManualReview || len(got.Warnings) == 0 {
		t.Fatalf("want manual review fallback, got %#v err=%v", got, err)
	}
}

func TestImageOCRExtractsCandidatesAndWarns(t *testing.T) {
	fakeTesseract(t, "HbA1c 6.2 %\nFerritin 45 ng/mL")
	got, err := PDFTextProcessor{Catalog: func(context.Context) []CatalogEntry { return testCatalog }}.Process(context.Background(), "image/jpeg", []byte("img"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ManualReview || got.PageCount != 1 || len(got.Pages) != 1 {
		t.Fatalf("unexpected result: %#v", got)
	}
	if len(got.Candidates) != 2 || got.Candidates[0].BiomarkerCode != "HBA1C" || len(got.UnknownNames) != 1 {
		t.Fatalf("unexpected candidates: %#v unknown=%v", got.Candidates, got.UnknownNames)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("OCR output must carry an unverified warning")
	}
}

func TestImageOCREmptyTextNeedsManualReview(t *testing.T) {
	fakeTesseract(t, "")
	got, err := PDFTextProcessor{}.Process(context.Background(), "image/png", []byte("img"))
	if err != nil || !got.ManualReview {
		t.Fatalf("want manual review, got %#v err=%v", got, err)
	}
}
