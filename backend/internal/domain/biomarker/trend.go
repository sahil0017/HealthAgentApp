package biomarker

import (
	"sort"
	"strings"
	"time"
)

type RangeInfo struct {
	Low    *float64 `json:"low"`
	High   *float64 `json:"high"`
	Unit   string   `json:"unit"`
	Origin string   `json:"origin"`
	Source string   `json:"source"`
}

type TrendPoint struct {
	ObservationID   string     `json:"observation_id"`
	ObservedAt      string     `json:"observed_at"`
	Value           float64    `json:"value"`
	Unit            string     `json:"unit"`
	OriginalValue   float64    `json:"original_value"`
	OriginalUnit    string     `json:"original_unit"`
	Converted       bool       `json:"converted"`
	MeasurementKind string     `json:"measurement_kind"`
	Verification    string     `json:"verification_status"`
	Revision        int        `json:"revision"`
	DocumentID      string     `json:"document_id,omitempty"`
	Confidence      string     `json:"source_confidence"`
	Flag            string     `json:"flag"`
	Range           *RangeInfo `json:"range"`
}

type ExcludedPoint struct {
	ObservationID string  `json:"observation_id"`
	ObservedAt    string  `json:"observed_at"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit"`
	Reason        string  `json:"reason"`
}

type CadenceInfo struct {
	TypicalGapDays *int `json:"typical_gap_days"`
	DaysSinceLast  *int `json:"days_since_last"`
	Overdue        bool `json:"overdue"`
}

type Trend struct {
	BiomarkerCode string          `json:"biomarker_code"`
	DisplayName   string          `json:"display_name"`
	CanonicalUnit string          `json:"canonical_unit"`
	Points        []TrendPoint    `json:"points"`
	Excluded      []ExcludedPoint `json:"excluded"`
	Trend         TrendResult     `json:"trend"`
	Cadence       CadenceInfo     `json:"cadence"`
	Unverified    int             `json:"unverified_count"`
	Kinds         []string        `json:"measurement_kinds"`
}

type ObsRow struct {
	ID, Name, Unit, LabRange, SourceType, Status string
	Value                                        float64
	On                                           time.Time
	Revision                                     int
	DocumentID                                   string
}

// MeasurementKind separates what a laboratory produced from what a device recorded, because the
// two are not interchangeable.
func MeasurementKind(sourceType string) string {
	switch t := strings.ToUpper(sourceType); {
	case t == "LAB_DOCUMENT":
		return "lab"
	case strings.HasPrefix(t, "DEVICE"):
		return "device"
	}
	return "other"
}

// BuildTrend turns one biomarker's observations into a comparable series in a single unit. The
// unit of the newest included reading is used, other readings are converted only through approved
// conversions, and anything that cannot be converted is listed as excluded instead of dropped.
func BuildTrend(code, name string, rows []ObsRow, includeUnverified bool, known map[ConversionKey]Conversion, learned []LearnedRange, sex *string, age *int, now time.Time) Trend {
	out := Trend{BiomarkerCode: code, DisplayName: name, Points: []TrendPoint{}, Excluded: []ExcludedPoint{}, Kinds: []string{}}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].On.Before(rows[j].On) })
	var usable []ObsRow
	for _, r := range rows {
		if r.Status != "VERIFIED" {
			out.Unverified++
			if !includeUnverified {
				continue
			}
		}
		usable = append(usable, r)
	}
	if len(usable) == 0 {
		return out
	}
	// Prefer the newest verified reading's unit so the series ends in what the user last saw.
	canonical := usable[len(usable)-1].Unit
	for i := len(usable) - 1; i >= 0; i-- {
		if usable[i].Status == "VERIFIED" {
			canonical = usable[i].Unit
			break
		}
	}
	out.CanonicalUnit = canonical
	kinds := map[string]bool{}
	var verified []Point
	var verifiedDates []time.Time
	for _, r := range usable {
		value, ok := ConvertValue(r.Value, r.Unit, canonical, known)
		if !ok {
			out.Excluded = append(out.Excluded, ExcludedPoint{ObservationID: r.ID, ObservedAt: r.On.Format("2006-01-02"), Value: r.Value, Unit: r.Unit, Reason: "no_approved_conversion"})
			continue
		}
		p := TrendPoint{ObservationID: r.ID, ObservedAt: r.On.Format("2006-01-02"), Value: value, Unit: canonical, OriginalValue: r.Value, OriginalUnit: r.Unit, Converted: r.Unit != canonical, MeasurementKind: MeasurementKind(r.SourceType), Verification: r.Status, Revision: r.Revision, DocumentID: r.DocumentID, Confidence: SourceConfidence(r.Status, r.SourceType)}
		p.Range, p.Flag = RangeFor(r, learned, known, sex, age)
		out.Points = append(out.Points, p)
		kinds[p.MeasurementKind] = true
		if r.Status == "VERIFIED" {
			verified = append(verified, Point{Value: value, On: r.On})
			verifiedDates = append(verifiedDates, r.On)
		}
	}
	for k := range kinds {
		out.Kinds = append(out.Kinds, k)
	}
	sort.Strings(out.Kinds)
	out.Trend = AnalyzeTrend(verified)
	if len(verifiedDates) > 0 {
		last := verifiedDates[len(verifiedDates)-1]
		since := int(now.Sub(last).Hours() / 24)
		out.Cadence.DaysSinceLast = &since
		if gap, ok := MeasurementCadence(verifiedDates); ok {
			days := int(gap.Hours()/24 + 0.5)
			out.Cadence.TypicalGapDays = &days
			out.Cadence.Overdue = OverdueForMeasurement(last, now, gap)
		}
	}
	return out
}

// RangeFor picks the range that applies to one reading: the lab's own printed range first, since
// it matches the method and the unit it was measured in, then an approved learned range.
func RangeFor(r ObsRow, learned []LearnedRange, known map[ConversionKey]Conversion, sex *string, age *int) (*RangeInfo, string) {
	if low, high, ok := ParseReferenceRange(r.LabRange); ok {
		return &RangeInfo{Low: low, High: high, Unit: r.Unit, Origin: "lab_report", Source: "Printed on the report"}, FlagValue(r.Value, low, high)
	}
	var usable []LearnedRange
	for _, l := range learned {
		if _, ok := ConvertValue(r.Value, r.Unit, l.Unit, known); ok {
			usable = append(usable, l)
		}
	}
	if pick := PickLearnedRange(usable, sex, age); pick != nil {
		value, _ := ConvertValue(r.Value, r.Unit, pick.Unit, known)
		return &RangeInfo{Low: pick.Low, High: pick.High, Unit: pick.Unit, Origin: "approved_reference", Source: pick.Source}, FlagValue(value, pick.Low, pick.High)
	}
	return nil, "unknown"
}
