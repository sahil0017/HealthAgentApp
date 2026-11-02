package processing

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	pdf "github.com/giraffesyo/pdf"
)

const PipelineVersion = "pdf-text-v2"

type Candidate struct {
	ID             string  `json:"id,omitempty"`
	BiomarkerCode  string  `json:"biomarker_code,omitempty"`
	Name           string  `json:"name"`
	Value          float64 `json:"value"`
	Unit           string  `json:"unit"`
	ObservedAt     *string `json:"observed_at,omitempty"`
	ReferenceRange string  `json:"reference_range"`
	Page           int     `json:"page"`
	Evidence       string  `json:"evidence"`
}

type Result struct {
	PageCount    int
	Pages        []string
	Text         string
	Candidates   []Candidate
	Warnings     []string
	UnknownNames []string
	ManualReview bool
}

// DocumentProcessor is the seam for deterministic document processing. Future AI
// integrations can implement it separately without changing document lifecycle code.
type DocumentProcessor interface {
	Process(context.Context, string, []byte) (Result, error)
}

// PDFTextProcessor reads selectable PDF text. Catalog supplies the analytes it may recognise.
type PDFTextProcessor struct {
	Catalog func(context.Context) []CatalogEntry
}

func (p PDFTextProcessor) Process(ctx context.Context, contentType string, data []byte) (Result, error) {
	if contentType != "application/pdf" {
		return Result{Warnings: []string{"Image OCR is not configured. Review this document manually."}, ManualReview: true}, nil
	}
	doc, err := pdf.Extract(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Result{}, fmt.Errorf("extract PDF text: %w", err)
	}
	var catalog []CatalogEntry
	if p.Catalog != nil {
		catalog = p.Catalog(ctx)
	}
	result := Result{PageCount: doc.PageCount}
	unknownSeen := map[string]bool{}
	for i, page := range doc.Pages {
		pageText := strings.TrimSpace(page.Text())
		result.Pages = append(result.Pages, pageText)
		if pageText != "" {
			found, unknown := ParseCandidates(pageText, i+1, catalog)
			result.Candidates = append(result.Candidates, found...)
			for _, u := range unknown {
				if !unknownSeen[u] {
					unknownSeen[u] = true
					result.UnknownNames = append(result.UnknownNames, u)
				}
			}
		}
	}
	result.Text = strings.TrimSpace(strings.Join(result.Pages, "\n\n"))
	if len(doc.Warnings) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("PDF parser reported %d recoverable issue(s); review the original report.", len(doc.Warnings)))
	}
	if result.Text == "" {
		result.ManualReview = true
		result.Warnings = append(result.Warnings, "No selectable text was found. This may be a scanned or unreadable PDF; review the original manually.")
	} else {
		result.Warnings = append(result.Warnings, "Extracted text and values are unverified. Compare every candidate with the original report before accepting.")
	}
	return result, nil
}

// CatalogEntry is an approved biomarker the extractor may recognise. Nothing is built in: an empty catalog finds no codes.
type CatalogEntry struct {
	Code  string
	Name  string
	Terms []string
	Units []string
}

// linePattern is a generic "name value unit" shape, so unseen analytes are still surfaced for review.
var linePattern = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9 ,'()\-]{1,60}?)\s*(?:[:=]\s*|\s{1,})([<>≤≥]?\s*\d+(?:[.,]\d+)?)\s*([A-Za-zµμ%][A-Za-z0-9µμ%/.^*\-]{0,19}(?:/[A-Za-zµμ0-9.^*\-]{1,12})?)`)

var nonTerm = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeTerm(s string) string {
	return strings.TrimSpace(nonTerm.ReplaceAllString(strings.ToLower(s), " "))
}

type index struct {
	terms map[string]CatalogEntry
	units map[string]bool
}

func buildIndex(catalog []CatalogEntry) index {
	ix := index{terms: map[string]CatalogEntry{}, units: map[string]bool{}}
	for _, e := range catalog {
		for _, t := range append([]string{e.Name}, e.Terms...) {
			if n := normalizeTerm(t); len(n) >= 2 {
				if _, taken := ix.terms[n]; !taken {
					ix.terms[n] = e
				}
			}
		}
		for _, u := range e.Units {
			ix.units[strings.ToLower(u)] = true
		}
	}
	return ix
}

// lookup matches the longest trailing run of words in the label, so "Fasting Glucose" resolves to a "glucose" term.
func (ix index) lookup(label string) (CatalogEntry, bool) {
	words := strings.Fields(normalizeTerm(label))
	for start := 0; start < len(words); start++ {
		if e, ok := ix.terms[strings.Join(words[start:], " ")]; ok {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

func plausibleUnit(unit string, known map[string]bool) bool {
	return strings.ContainsAny(unit, "/%") || known[strings.ToLower(unit)]
}

// ParseCandidates returns candidates for matched catalog analytes and, separately, labels it could not match.
func ParseCandidates(text string, page int, catalog []CatalogEntry) (candidates []Candidate, unknown []string) {
	ix := buildIndex(catalog)
	candidates = make([]Candidate, 0)
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		for _, match := range linePattern.FindAllStringSubmatch(line, -1) {
			label, raw := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
			unit := strings.ReplaceAll(strings.TrimSpace(match[3]), "μ", "µ")
			if strings.ContainsAny(raw, "<>≤≥") || len(normalizeTerm(label)) < 2 || !plausibleUnit(unit, ix.units) {
				continue
			}
			value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			c := Candidate{Name: label, Value: value, Unit: unit, Page: page, Evidence: strings.TrimSpace(line)}
			if e, ok := ix.lookup(label); ok {
				c.BiomarkerCode, c.Name = e.Code, e.Name
			} else if key := normalizeTerm(label); !seen[key] && len(key) <= 60 {
				seen[key] = true
				unknown = append(unknown, label)
			}
			candidates = append(candidates, c)
		}
	}
	return candidates, unknown
}
