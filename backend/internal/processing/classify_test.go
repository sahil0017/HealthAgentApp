package processing

import "testing"

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"Laboratory Report\nHemoglobin 13.2 g/dL\nReference range 12-16":             "LAB_REPORT",
		"Discharge Summary\nDate of admission 1 May\nDischarge diagnosis: pneumonia": "DISCHARGE_SUMMARY",
		"Rx Amoxicillin capsule 500 mg twice daily":                                  "PRESCRIPTION",
		"Radiology\nFindings: clear lungs\nImpression: normal chest X-ray":           "RADIOLOGY_REPORT",
		"This is to certify that the patient needs sick leave, rest for 3 days":      "MEDICAL_CERTIFICATE",
		"Grocery list: eggs, milk":                                                   "OTHER",
		"":                                                                           "OTHER",
	}
	for text, want := range cases {
		if got := Classify(text); got.Type != want {
			t.Errorf("Classify(%q) = %s, want %s", text, got.Type, want)
		}
	}
	if c := Classify("Laboratory Report reference range glucose mg/dl"); c.Confidence <= 0 || c.Confidence > 1 {
		t.Errorf("confidence out of range: %v", c.Confidence)
	}
}
