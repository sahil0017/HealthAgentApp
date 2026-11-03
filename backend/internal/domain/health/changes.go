package health

import (
	"cortex/backend/internal/domain/biomarker"
	"strings"
)

type ChangeSummary struct {
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

// SummarizeChange describes the newest reading and how it moved from the one before.
// The change is only reported when both readings use the same unit.
func SummarizeChange(code, name string, readings []biomarker.Reading) (ChangeSummary, bool) {
	if len(readings) == 0 {
		return ChangeSummary{}, false
	}
	latest := readings[len(readings)-1]
	out := ChangeSummary{BiomarkerCode: code, Name: name, Value: latest.Value, Unit: latest.Unit, ObservedOn: latest.On.Format("2006-01-02"), TrackedReading: len(readings)}
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
