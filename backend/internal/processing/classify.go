package processing

import "strings"

// Classification is a suggested document type. It is never authoritative: the owner can always override it.
type Classification struct {
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

var classificationCues = []struct {
	docType string
	cues    []string
}{
	{"DISCHARGE_SUMMARY", []string{"discharge summary", "date of discharge", "discharge diagnosis", "discharge medication", "hospital course", "date of admission"}},
	{"RADIOLOGY_REPORT", []string{"radiology", "impression:", "findings:", "x-ray", "ct scan", "mri", "ultrasound", "sonography", "radiologist", "technique:"}},
	{"PRESCRIPTION", []string{"prescription", "rx", "tablet", "capsule", "dosage", "twice daily", "once daily", "sig:", "refill", "dispense"}},
	{"MEDICAL_CERTIFICATE", []string{"medical certificate", "fitness certificate", "certify that", "is fit to", "is unfit", "sick leave", "rest for"}},
	{"VACCINATION_RECORD", []string{"vaccination", "immunization", "immunisation", "vaccine", "dose administered", "batch no", "lot number"}},
	{"LAB_REPORT", []string{"laboratory", "lab report", "reference range", "reference interval", "specimen", "pathology", "hemoglobin", "haemoglobin", "glucose", "cholesterol", "creatinine", "result", "mg/dl", "mmol/l", "g/dl"}},
}

// Classify scores keyword cues, so it is deterministic and explainable. Text with no clear signal stays OTHER.
func Classify(text string) Classification {
	lower := strings.ToLower(text)
	best, bestHits, total := "OTHER", 0, 0
	for _, group := range classificationCues {
		hits := 0
		for _, cue := range group.cues {
			if strings.Contains(lower, cue) {
				hits++
			}
		}
		total += hits
		if hits > bestHits {
			best, bestHits = group.docType, hits
		}
	}
	if bestHits < 2 {
		return Classification{Type: "OTHER"}
	}
	return Classification{Type: best, Confidence: float64(bestHits) / float64(total)}
}
