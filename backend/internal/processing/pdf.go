package processing

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	pdf "github.com/giraffesyo/pdf"
)

const PipelineVersion = "pdf-text-ocr-ai-v4"

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
	Panel          string  `json:"panel,omitempty"`
}

type Result struct {
	PageCount    int
	Pages        []string
	Text         string
	Candidates   []Candidate
	Warnings     []string
	UnknownNames []string
	ManualReview bool
	// AI is the model's reading of the document, set by the worker when the person allowed the cloud model.
	AI      *AIReading
	AIAdded int
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
		return p.processImage(ctx, data)
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
		if OCRAvailable() && rasterAvailable() {
			pages, err := ocrScannedPDF(ctx, data, doc.PageCount)
			if err != nil {
				return Result{}, err
			}
			ocr := p.fromOCR(ctx, pages)
			if doc.PageCount > maxScannedPages {
				ocr.Warnings = append(ocr.Warnings, fmt.Sprintf("Only the first %d of %d pages were read. Review the remaining pages in the original.", maxScannedPages, doc.PageCount))
			}
			return ocr, nil
		}
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

var parenthetical = regexp.MustCompile(`\([^)]*\)?`)

// exact matches the whole label (with or without a bracketed abbreviation). Table rows use it because a
// partial match would file "Platelet Hematocrit" under Hematocrit; an unmatched row is left for the user to map.
func (ix index) exact(label string) (CatalogEntry, bool) {
	for _, l := range []string{label, parenthetical.ReplaceAllString(label, " ")} {
		if e, ok := ix.terms[normalizeTerm(l)]; ok {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

var footnote = regexp.MustCompile(`(?i)\b(individuals|up to|certain|rhythm|usually|approximately)\b`)

func plausibleUnit(unit string, known map[string]bool) bool {
	return strings.ContainsAny(unit, "/%") || known[strings.ToLower(unit)]
}

// ParseCandidates returns candidates for matched catalog analytes and, separately, labels it could not match.
func ParseCandidates(text string, page int, catalog []CatalogEntry) (candidates []Candidate, unknown []string) {
	ix := buildIndex(catalog)
	candidates = make([]Candidate, 0)
	seen := map[string]bool{}
	observedAt := ReportDate(text)
	lines := strings.Split(text, "\n")
	for li, line := range lines {
		if c, ok := tableRow(lines, li, ix); ok {
			c.Page, c.ObservedAt = page, observedAt
			if e, found := ix.exact(c.Name); found {
				c.BiomarkerCode, c.Name = e.Code, e.Name
			} else if key := normalizeTerm(c.Name); !seen[key] && len(key) <= 60 {
				seen[key] = true
				unknown = append(unknown, c.Name)
			}
			candidates = append(candidates, c)
			continue
		}
		matches := linePattern.FindAllStringSubmatchIndex(line, -1)
		for mi, loc := range matches {
			match := make([]string, 4)
			for g := range match {
				match[g] = line[loc[2*g]:loc[2*g+1]]
			}
			tailEnd := len(line)
			if mi+1 < len(matches) {
				tailEnd = matches[mi+1][0]
			}
			label, raw := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
			unit := strings.ReplaceAll(strings.TrimSpace(match[3]), "μ", "µ")
			if strings.ContainsAny(raw, "<>≤≥") || len(normalizeTerm(label)) < 2 || !plausibleUnit(unit, ix.units) {
				continue
			}
			value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			c := Candidate{Name: label, Value: value, Unit: unit, Page: page, Evidence: strings.TrimSpace(line), ObservedAt: observedAt, ReferenceRange: referenceRange(line[loc[1]:tailEnd])}
			if e, ok := ix.lookup(label); ok {
				c.BiomarkerCode, c.Name = e.Code, e.Name
			} else if key := normalizeTerm(label); !seen[key] && len(key) <= 60 {
				seen[key] = true
				unknown = append(unknown, label)
			}
			candidates = append(candidates, c)
		}
	}
	// Two different rows claiming one analyte means the catalog match is unreliable; leave both for the user to map.
	claims := map[string]int{}
	for _, c := range candidates {
		if c.BiomarkerCode != "" {
			claims[c.BiomarkerCode]++
		}
	}
	for i, c := range candidates {
		if c.BiomarkerCode != "" && claims[c.BiomarkerCode] > 1 {
			candidates[i].BiomarkerCode = ""
		}
	}
	return candidates, unknown
}

// rowPattern is a lab-table row: "Name  value  reference range" with the unit printed on a following line.
var rowPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9 ,'()\-./]{1,70}?)\s+(\d+(?:\.\d+)?)\s+(\d+(?:\.\d+)?\s*[-–—]\s*\d+(?:\.\d+)?|[<>≤≥]=?\s*\d+(?:\.\d+)?)\s*$`)

// tableRow reads a row whose unit sits on one of the next two lines, and joins a label wrapped across lines.
func tableRow(lines []string, i int, ix index) (Candidate, bool) {
	line := strings.TrimSpace(lines[i])
	m := rowPattern.FindStringSubmatch(line)
	if m == nil {
		return Candidate{}, false
	}
	value, err := strconv.ParseFloat(m[2], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return Candidate{}, false
	}
	label := strings.TrimSpace(m[1])
	unit, wrapped := "", ""
	for j := i + 1; j <= i+2 && j < len(lines); j++ {
		next := strings.TrimSpace(lines[j])
		fields := strings.Fields(next)
		if len(fields) == 0 {
			continue
		}
		last := strings.ReplaceAll(fields[len(fields)-1], "μ", "µ")
		if plausibleUnit(last, ix.units) && !rowPattern.MatchString(next) {
			unit = last
			if j == i+1 {
				wrapped = strings.Join(fields[:len(fields)-1], " ")
			}
			break
		}
	}
	if unit == "" || len(normalizeTerm(label)) < 2 || len(strings.Fields(label)) > 8 || footnote.MatchString(label) {
		return Candidate{}, false
	}
	if wrapped != "" {
		joined := label + " " + wrapped
		_, joinedKnown := ix.exact(joined)
		_, plainKnown := ix.exact(label)
		if joinedKnown || (!plainKnown && strings.Count(label, "(") > strings.Count(label, ")")) {
			label = joined
		}
	}
	return Candidate{Name: label, Value: value, Unit: unit, Evidence: line, ReferenceRange: strings.Join(strings.Fields(m[3]), " ")}, true
}

var rangePattern = regexp.MustCompile(`(\d+(?:[.,]\d+)?\s*(?:-|–|—|to)\s*\d+(?:[.,]\d+)?|[<>≤≥]=?\s*\d+(?:[.,]\d+)?)`)

// referenceRange returns a range printed on the report itself, right after the result.
func referenceRange(tail string) string {
	m := rangePattern.FindString(tail)
	return strings.Join(strings.Fields(m), " ")
}

var (
	isoDate       = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	monthDate     = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2}),?\s+(\d{4})\b`)
	slashDate     = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b`)
	dateLabelHint = regexp.MustCompile(`(?i)collect|report|specimen|drawn|sample|received|date of (service|test)`)
	months        = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

// ReportDate returns a date only from a line labelled as a collection or report date, so birth dates are never mistaken for it.
func ReportDate(text string) *string {
	for _, line := range strings.Split(text, "\n") {
		if !dateLabelHint.MatchString(line) {
			continue
		}
		var y, mo, d int
		if m := isoDate.FindStringSubmatch(line); m != nil {
			y, _ = strconv.Atoi(m[1])
			mo, _ = strconv.Atoi(m[2])
			d, _ = strconv.Atoi(m[3])
		} else if m := monthDate.FindStringSubmatch(line); m != nil {
			mo = months[strings.ToLower(m[1])]
			d, _ = strconv.Atoi(m[2])
			y, _ = strconv.Atoi(m[3])
		} else if m := slashDate.FindStringSubmatch(line); m != nil {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			y, _ = strconv.Atoi(m[3])
			switch {
			case a > 12:
				d, mo = a, b
			case b > 12:
				mo, d = a, b
			default: // 03/04/2026 could be either order, so no date is better than a wrong one
				continue
			}
		} else {
			continue
		}
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
		if mo < 1 || mo > 12 || t.Day() != d || y < 1990 || t.After(time.Now().AddDate(0, 0, 1)) {
			continue
		}
		out := t.Format("2006-01-02")
		return &out
	}
	return nil
}
