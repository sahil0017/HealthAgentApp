package processing

import "testing"

var aiCatalog = []CatalogEntry{{Code: "HEMOGLOBIN", Name: "Hemoglobin", Terms: []string{"haemoglobin", "hb"}, Units: []string{"g/dL"}}}

func TestMergeAIAddsOnlyRowsThePageSupports(t *testing.T) {
	page := "Test Name\nHaemoglobin\n13.2\ng/dL\n12.0 - 15.0\nVitamin D 25 OH\n18.4 ng/mL"
	r := &Result{}
	added := MergeAI(r, 1, page, AIReading{Tests: []AITest{
		{Name: "Haemoglobin", Value: "13.2", Unit: "g/dL", ReferenceRange: "12.0 - 15.0"},
		{Name: "Vitamin D 25 OH", Value: "18.4", Unit: "ng/mL", Panel: "Vitamins"},
		{Name: "Ferritin", Value: "55", Unit: "ng/mL"},   // not on the page
		{Name: "Haemoglobin", Value: "99", Unit: "g/dL"}, // wrong value
	}}, aiCatalog)
	if added != 2 || len(r.Candidates) != 2 {
		t.Fatalf("added %d, candidates %+v", added, r.Candidates)
	}
	if r.Candidates[0].BiomarkerCode != "HEMOGLOBIN" || r.Candidates[0].Evidence == "" {
		t.Fatalf("first candidate %+v", r.Candidates[0])
	}
	if r.Candidates[1].BiomarkerCode != "" || r.Candidates[1].Panel != "Vitamins" || len(r.UnknownNames) != 1 {
		t.Fatalf("second candidate %+v unknown %v", r.Candidates[1], r.UnknownNames)
	}
}

func TestMergeAISkipsResultsAlreadyFound(t *testing.T) {
	page := "Haemoglobin 13.2 g/dL 12.0 - 15.0"
	r := &Result{}
	r.Candidates, _ = ParseCandidates(page, 1, aiCatalog)
	before := len(r.Candidates)
	if n := MergeAI(r, 1, page, AIReading{Tests: []AITest{{Name: "Haemoglobin", Value: "13.2", Unit: "g/dL"}}}, aiCatalog); n != 0 || len(r.Candidates) != before {
		t.Fatalf("duplicate added: %d %+v", n, r.Candidates)
	}
}

func TestMergeAIRejectsNonNumericValues(t *testing.T) {
	r := &Result{}
	if n := MergeAI(r, 1, "Colour Pale Yellow", AIReading{Tests: []AITest{{Name: "Colour", Value: "Pale Yellow"}}}, nil); n != 0 {
		t.Fatalf("added %d", n)
	}
}

func TestScrubIdentityRemovesPersonalDetails(t *testing.T) {
	in := "Patient Name: Asha Rao\nMobile: +91 98765 43210\nReport to asha@example.com\nHaemoglobin 13.2 g/dL\nDr. Mehta MD\nAge/Sex: 34 / F"
	out := ScrubIdentity(in, false)
	for _, bad := range []string{"Asha", "98765", "example.com", "Mehta", "34 / F"} {
		if contains(out, bad) {
			t.Fatalf("%q kept in %q", bad, out)
		}
	}
	if !contains(out, "Haemoglobin 13.2 g/dL") {
		t.Fatalf("result removed: %q", out)
	}
	if !contains(ScrubIdentity(in, true), "Dr. Mehta") {
		t.Fatal("prescriber must be kept for prescriptions")
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
