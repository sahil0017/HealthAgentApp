package processing

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The worker image installs both tools; tests replace them with fakes.
var (
	ocrBinary    = "tesseract"
	rasterBinary = "pdftoppm"
)

const (
	maxScannedPages = 10
	rasterDPI       = "200"
)

var ocrLanguagePattern = regexp.MustCompile(`^[a-z]{3}(_[a-z]+)?(\+[a-z]{3}(_[a-z]+)?)*$`)

// ocrLanguages reads CORTEX_OCR_LANGS (for example "eng+hin"); anything malformed falls back to English.
func ocrLanguages() string {
	v := strings.TrimSpace(os.Getenv("CORTEX_OCR_LANGS"))
	if ocrLanguagePattern.MatchString(v) {
		return v
	}
	return "eng"
}

// OCRAvailable reports whether Tesseract is installed in this environment.
func OCRAvailable() bool {
	_, err := exec.LookPath(ocrBinary)
	return err == nil
}

func rasterAvailable() bool {
	_, err := exec.LookPath(rasterBinary)
	return err == nil
}

func ocrImageBytes(ctx context.Context, data []byte) (string, error) {
	cmd := exec.CommandContext(ctx, ocrBinary, "stdin", "stdout", "-l", ocrLanguages(), "--psm", "6")
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ocr image: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// processImage OCRs a JPEG/PNG. Without Tesseract it falls back to manual review.
func (p PDFTextProcessor) processImage(ctx context.Context, data []byte) (Result, error) {
	if !OCRAvailable() {
		return Result{Warnings: []string{"Image OCR is not configured. Review this document manually."}, ManualReview: true}, nil
	}
	text, err := ocrImageBytes(ctx, data)
	if err != nil {
		return Result{}, err
	}
	return p.fromOCR(ctx, []string{text}), nil
}

// ocrScannedPDF renders each page to an image and OCRs it, for PDFs that have no selectable text.
func ocrScannedPDF(ctx context.Context, data []byte, pageCount int) ([]string, error) {
	dir, err := os.MkdirTemp("", "cortex-ocr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		return nil, err
	}
	last := maxScannedPages
	if pageCount > 0 && pageCount < last {
		last = pageCount
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, rasterBinary, "-png", "-r", rasterDPI, "-f", "1", "-l", fmt.Sprint(last), input, filepath.Join(dir, "page"))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("render PDF pages: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	files, err := filepath.Glob(filepath.Join(dir, "page-*.png"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files) // pdftoppm zero-pads page numbers, so name order is page order
	pages := make([]string, 0, len(files))
	for _, f := range files {
		img, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		text, err := ocrImageBytes(ctx, img)
		if err != nil {
			return nil, err
		}
		pages = append(pages, text)
	}
	return pages, nil
}

// fromOCR builds a Result from OCR'd pages. OCR output is noisier than PDF text, so every value stays flagged for review.
func (p PDFTextProcessor) fromOCR(ctx context.Context, pages []string) Result {
	result := Result{PageCount: len(pages), Pages: pages}
	var catalog []CatalogEntry
	if p.Catalog != nil {
		catalog = p.Catalog(ctx)
	}
	seen := map[string]bool{}
	for i, page := range pages {
		if page == "" {
			continue
		}
		found, unknown := ParseCandidates(page, i+1, catalog)
		result.Candidates = append(result.Candidates, found...)
		for _, u := range unknown {
			if !seen[u] {
				seen[u] = true
				result.UnknownNames = append(result.UnknownNames, u)
			}
		}
	}
	result.Text = strings.TrimSpace(strings.Join(pages, "\n\n"))
	if result.Text == "" {
		result.ManualReview = true
		result.Warnings = []string{"OCR found no readable text. Review the original manually."}
		return result
	}
	result.Warnings = []string{"Text was read by OCR and may contain recognition errors. Compare every value with the original before accepting."}
	return result
}
