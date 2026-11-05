package processing

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// AITest is one result the language model read from a page.
type AITest struct {
	Name           string `json:"name"`
	Value          string `json:"value"`
	Unit           string `json:"unit"`
	ReferenceRange string `json:"reference_range"`
	Panel          string `json:"panel"`
}

// AIMedicine is one prescribed medicine the model read, exactly as written.
type AIMedicine struct {
	Name         string `json:"name"`
	Strength     string `json:"strength"`
	Form         string `json:"form"`
	Dose         string `json:"dose"`
	Frequency    string `json:"frequency"`
	Timing       string `json:"timing"`
	Duration     string `json:"duration"`
	Instructions string `json:"instructions"`
}

// AIReading is the model's structured reading of a document.
type AIReading struct {
	DocumentType string       `json:"document_type"`
	Provider     string       `json:"provider"`
	ReportDate   string       `json:"report_date"`
	Doctor       string       `json:"doctor"`
	Tests        []AITest     `json:"tests"`
	Medicines    []AIMedicine `json:"medicines"`
}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	phonePattern = regexp.MustCompile(`(?:\+?\d[\s\-]?){9,}`)
	identityLine = regexp.MustCompile(`(?i)^\s*(patient|name|mr\.?|mrs\.?|ms\.?|address|mobile|phone|contact|email|uhid|mrn|pid|reg(istration)?\.? ?(no|id)|sample id|barcode|lab id|visit id|order id|ref(erred)?\.? ?(by|doctor)|client|collected at|age\b|sex\b|gender|dob|date of birth)\b`)
)

// ScrubIdentity removes lines and fragments that identify the person before text is sent to a model. The
// results and test names are all the model needs. A prescriber is kept only when keepDoctor is set.
func ScrubIdentity(text string, keepDoctor bool) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if identityLine.MatchString(line) {
			continue
		}
		if !keepDoctor && regexp.MustCompile(`(?i)\bdr\.?\s`).MatchString(line) && !regexp.MustCompile(`\d`).MatchString(line) {
			continue
		}
		line = emailPattern.ReplaceAllString(line, "")
		line = phonePattern.ReplaceAllString(line, "")
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var numberToken = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

func sameNumber(a, b string) bool {
	x, e1 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(a), ",", "."), 64)
	y, e2 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(b), ",", "."), 64)
	return e1 == nil && e2 == nil && math.Abs(x-y) < 1e-9
}

// evidenceFor finds the line, or the line pair, of the page that really contains both the test name and the
// value. A row the model produced without such a line is dropped: nothing it adds can be unsupported by the document.
func evidenceFor(lines []string, name, value string) (string, bool) {
	words := strings.Fields(normalizeTerm(name))
	if len(words) == 0 {
		return "", false
	}
	has := func(s string) bool {
		n := " " + normalizeTerm(s) + " "
		for _, w := range words[:min(len(words), 2)] {
			if !strings.Contains(n, " "+w) {
				return false
			}
		}
		return true
	}
	hasValue := func(s string) bool {
		for _, tok := range numberToken.FindAllString(s, -1) {
			if sameNumber(tok, value) {
				return true
			}
		}
		return false
	}
	for i, l := range lines {
		for span := 1; span <= 3 && i+span <= len(lines); span++ {
			joined := strings.Join(lines[i:i+span], " ")
			if has(l) && hasValue(joined) {
				return strings.TrimSpace(joined), true
			}
		}
	}
	return "", false
}

// MergeAI adds model-read results that the pattern parser missed. Each one must be backed by a line of the page,
// must have a numeric value and unit, and must not duplicate a result already found. It returns how many were added.
func MergeAI(r *Result, pageIdx int, pageText string, ai AIReading, catalog []CatalogEntry) int {
	ix := buildIndex(catalog)
	lines := strings.Split(pageText, "\n")
	observed := ReportDate(pageText)
	if observed == nil && len(ai.ReportDate) == 10 {
		d := ai.ReportDate
		observed = &d
	}
	have := map[string]bool{}
	for _, c := range r.Candidates {
		have[normalizeTerm(c.Name)+"|"+strconv.FormatFloat(c.Value, 'f', -1, 64)] = true
	}
	added := 0
	for _, t := range ai.Tests {
		name := strings.TrimSpace(t.Name)
		val, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(t.Value), ",", "."), 64)
		unit := strings.ReplaceAll(strings.TrimSpace(t.Unit), "μ", "µ")
		if name == "" || len(name) > 100 || IsRangeText(name) || err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		evidence, ok := evidenceFor(lines, name, t.Value)
		if !ok {
			continue
		}
		if have[normalizeTerm(name)+"|"+strconv.FormatFloat(val, 'f', -1, 64)] {
			continue
		}
		dupe := false
		for _, c := range r.Candidates {
			if c.Page == pageIdx && sameNumber(strconv.FormatFloat(c.Value, 'f', -1, 64), t.Value) && strings.Contains(" "+normalizeTerm(c.Evidence)+" ", " "+leadWords(name)+" ") {
				dupe = true
				break
			}
		}
		if dupe {
			continue
		}
		c := Candidate{Name: name, Value: val, Unit: unit, ObservedAt: observed, ReferenceRange: strings.TrimSpace(t.ReferenceRange), Page: pageIdx, Evidence: evidence, Panel: strings.TrimSpace(t.Panel)}
		if e, found := ix.exact(name); found {
			c.BiomarkerCode, c.Name = e.Code, e.Name
		} else if e, found := ix.lookup(name); found && len(strings.Fields(normalizeTerm(name))) <= len(strings.Fields(normalizeTerm(e.Name)))+1 {
			c.BiomarkerCode, c.Name = e.Code, e.Name
		}
		r.Candidates = append(r.Candidates, c)
		if c.BiomarkerCode == "" {
			r.UnknownNames = append(r.UnknownNames, c.Name)
		}
		added++
	}
	return added
}

func leadWords(s string) string {
	w := strings.Fields(normalizeTerm(s))
	return strings.Join(w[:min(len(w), 2)], " ")
}
