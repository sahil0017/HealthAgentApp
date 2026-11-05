package biomarker

import "testing"

func TestCodeFor(t *testing.T) {
	for in, want := range map[string]string{
		"Total Cholesterol":      "TOTAL_CHOLESTEROL",
		" Vitamin B-12 (Serum) ": "VITAMIN_B_12_SERUM",
		"ESR":                    "ESR",
		"%":                      "",
		"x":                      "",
	} {
		if got := CodeFor(in); got != want {
			t.Errorf("CodeFor(%q) = %q, want %q", in, got, want)
		}
	}
}
