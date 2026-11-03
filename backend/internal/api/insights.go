package api

import (
	"sort"
	"strings"
	"time"
)

// reading is one verified measurement of a biomarker.
type reading struct {
	Value float64
	Unit  string
	On    time.Time
}

type goalTarget struct {
	Min  *float64
	Max  *float64
	Unit string
}

type goalEvaluation struct {
	Status string `json:"status"` // no_data, unit_mismatch, on_target, below_target, above_target
	Trend  string `json:"trend"`  // improving, worsening, steady, unknown
}

func distanceFromTarget(t goalTarget, value float64) float64 {
	switch {
	case t.Min != nil && value < *t.Min:
		return *t.Min - value
	case t.Max != nil && value > *t.Max:
		return value - *t.Max
	}
	return 0
}

// evaluateGoal compares the latest reading with the user's own target. It never compares
// values recorded in different units, because converting them would need clinical rules.
func evaluateGoal(t goalTarget, latest, previous *reading) goalEvaluation {
	if latest == nil {
		return goalEvaluation{Status: "no_data", Trend: "unknown"}
	}
	if !strings.EqualFold(strings.TrimSpace(latest.Unit), strings.TrimSpace(t.Unit)) {
		return goalEvaluation{Status: "unit_mismatch", Trend: "unknown"}
	}
	status := "on_target"
	switch {
	case t.Min != nil && latest.Value < *t.Min:
		status = "below_target"
	case t.Max != nil && latest.Value > *t.Max:
		status = "above_target"
	}
	trend := "unknown"
	if previous != nil && strings.EqualFold(strings.TrimSpace(previous.Unit), strings.TrimSpace(t.Unit)) {
		before, now := distanceFromTarget(t, previous.Value), distanceFromTarget(t, latest.Value)
		switch {
		case now < before:
			trend = "improving"
		case now > before:
			trend = "worsening"
		default:
			trend = "steady"
		}
	}
	return goalEvaluation{Status: status, Trend: trend}
}

// measurementCadence is the person's own typical gap between measurements (median),
// learned from their history. It needs at least three distinct dates.
func measurementCadence(dates []time.Time) (time.Duration, bool) {
	days := map[string]time.Time{}
	for _, d := range dates {
		days[d.Format("2006-01-02")] = d
	}
	if len(days) < 3 {
		return 0, false
	}
	sorted := make([]time.Time, 0, len(days))
	for _, d := range days {
		sorted = append(sorted, d)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	gaps := make([]time.Duration, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		gaps = append(gaps, sorted[i].Sub(sorted[i-1]))
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	mid := len(gaps) / 2
	if len(gaps)%2 == 1 {
		return gaps[mid], true
	}
	return (gaps[mid-1] + gaps[mid]) / 2, true
}

// overdueForMeasurement is true once the wait since the last reading is more than twice
// the person's usual gap.
func overdueForMeasurement(last, now time.Time, cadence time.Duration) bool {
	return cadence > 0 && now.Sub(last) > 2*cadence
}

type changeSummary struct {
	BiomarkerCode  string   `json:"biomarker_code"`
	Name           string   `json:"name"`
	Value          float64  `json:"value"`
	Unit           string   `json:"unit"`
	ObservedOn     string   `json:"observed_on"`
	PreviousValue  *float64 `json:"previous_value"`
	PreviousOn     *string  `json:"previous_observed_on"`
	Change         *float64 `json:"change"`
	ChangePercent  *float64 `json:"change_percent"`
	TrackedReading int      `json:"reading_count"`
}

// summarizeChange describes the newest reading and how it moved from the one before.
// The change is only reported when both readings use the same unit.
func summarizeChange(code, name string, readings []reading) (changeSummary, bool) {
	if len(readings) == 0 {
		return changeSummary{}, false
	}
	latest := readings[len(readings)-1]
	out := changeSummary{BiomarkerCode: code, Name: name, Value: latest.Value, Unit: latest.Unit, ObservedOn: latest.On.Format("2006-01-02"), TrackedReading: len(readings)}
	if len(readings) > 1 {
		prev := readings[len(readings)-2]
		day := prev.On.Format("2006-01-02")
		out.PreviousValue, out.PreviousOn = &prev.Value, &day
		if strings.EqualFold(prev.Unit, latest.Unit) {
			change := latest.Value - prev.Value
			out.Change = &change
			if prev.Value != 0 {
				percent := change / prev.Value * 100
				out.ChangePercent = &percent
			}
		}
	}
	return out, true
}

type completenessItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Done   bool   `json:"done"`
	Detail string `json:"detail"`
}

type completeness struct {
	Score int                `json:"score"`
	Items []completenessItem `json:"items"`
}

type completenessFacts struct {
	StorageConsent      bool
	ProfileDetails      bool
	ProcessingConsent   bool
	Documents           int
	DocumentsToReview   int
	ObservationsPending int
	HistoryEntries      int
	ActiveGoals         int
}

// assessCompleteness measures how complete the person's record is, not how healthy they are.
func assessCompleteness(f completenessFacts) completeness {
	items := []completenessItem{
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
	return completeness{Score: done * 100 / len(items), Items: items}
}

type suggestion struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Reason  string `json:"reason"`
	RefType string `json:"ref_type"`
	RefID   string `json:"ref_id"`
}

// withoutExisting drops suggestions that match an open follow-up by title, so the same
// prompt is never offered twice.
func withoutExisting(candidates []suggestion, openTitles []string) []suggestion {
	taken := map[string]bool{}
	for _, title := range openTitles {
		taken[strings.ToLower(strings.TrimSpace(title))] = true
	}
	out := []suggestion{}
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c.Title))
		if taken[key] {
			continue
		}
		taken[key] = true
		out = append(out, c)
	}
	return out
}

// ageYears returns the whole years between a birth date and today.
func ageYears(birth, today time.Time) int {
	years := today.Year() - birth.Year()
	if today.Month() < birth.Month() || (today.Month() == birth.Month() && today.Day() < birth.Day()) {
		years--
	}
	return years
}
