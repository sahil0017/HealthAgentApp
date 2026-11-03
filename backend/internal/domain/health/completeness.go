package health

type CompletenessItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Done   bool   `json:"done"`
	Detail string `json:"detail"`
}

type Completeness struct {
	Score int                `json:"score"`
	Items []CompletenessItem `json:"items"`
}

type CompletenessFacts struct {
	StorageConsent      bool
	ProfileDetails      bool
	ProcessingConsent   bool
	Documents           int
	DocumentsToReview   int
	ObservationsPending int
	HistoryEntries      int
	ActiveGoals         int
}

// AssessCompleteness measures how complete the person's record is, not how healthy they are.
func AssessCompleteness(f CompletenessFacts) Completeness {
	items := []CompletenessItem{
		{"consent", "Data permissions chosen", f.StorageConsent && f.ProcessingConsent, "Allow report storage and processing so CORTEX can organise your records."},
		{"documents", "At least one report added", f.Documents > 0, "Upload a lab report, prescription or summary."},
		{"reviewed", "Reports reviewed", f.Documents > 0 && f.DocumentsToReview == 0, "Check extracted values before they join your record."},
		{"verified", "Values verified", f.Documents > 0 && f.ObservationsPending == 0, "Some values are still waiting for verification."},
		{"profile", "Health profile filled in", f.ProfileDetails, "Add your date of birth and sex so results can be read in context."},
		{"history", "Medical history added", f.HistoryEntries > 0, "Add conditions, allergies, surgeries or medicines."},
		{"goals", "A goal set", f.ActiveGoals > 0, "Choose a value you want to track against a target."},
	}
	done := 0
	for _, item := range items {
		if item.Done {
			done++
		}
	}
	return Completeness{Score: done * 100 / len(items), Items: items}
}
