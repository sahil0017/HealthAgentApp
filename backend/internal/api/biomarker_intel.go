package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type rangeInfo struct {
	Low    *float64 `json:"low"`
	High   *float64 `json:"high"`
	Unit   string   `json:"unit"`
	Origin string   `json:"origin"`
	Source string   `json:"source"`
}

type trendPoint struct {
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
	Flag            string     `json:"flag"`
	Range           *rangeInfo `json:"range"`
}

type excludedPoint struct {
	ObservationID string  `json:"observation_id"`
	ObservedAt    string  `json:"observed_at"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit"`
	Reason        string  `json:"reason"`
}

type cadenceInfo struct {
	TypicalGapDays *int `json:"typical_gap_days"`
	DaysSinceLast  *int `json:"days_since_last"`
	Overdue        bool `json:"overdue"`
}

type biomarkerTrend struct {
	BiomarkerCode string          `json:"biomarker_code"`
	DisplayName   string          `json:"display_name"`
	CanonicalUnit string          `json:"canonical_unit"`
	Points        []trendPoint    `json:"points"`
	Excluded      []excludedPoint `json:"excluded"`
	Trend         trendResult     `json:"trend"`
	Cadence       cadenceInfo     `json:"cadence"`
	Unverified    int             `json:"unverified_count"`
	Kinds         []string        `json:"measurement_kinds"`
}

type obsRow struct {
	ID, Name, Unit, LabRange, SourceType, Status string
	Value                                        float64
	On                                           time.Time
	Revision                                     int
}

// measurementKind separates what a laboratory produced from what a device recorded, because the
// two are not interchangeable.
func measurementKind(sourceType string) string {
	switch t := strings.ToUpper(sourceType); {
	case t == "LAB_DOCUMENT":
		return "lab"
	case strings.HasPrefix(t, "DEVICE"):
		return "device"
	}
	return "other"
}

// buildTrend turns one biomarker's observations into a comparable series in a single unit. The
// unit of the newest included reading is used, other readings are converted only through approved
// conversions, and anything that cannot be converted is listed as excluded instead of dropped.
func buildTrend(code, name string, rows []obsRow, includeUnverified bool, known map[conversionKey]conversion, learned []learnedRange, sex *string, age *int, now time.Time) biomarkerTrend {
	out := biomarkerTrend{BiomarkerCode: code, DisplayName: name, Points: []trendPoint{}, Excluded: []excludedPoint{}, Kinds: []string{}}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].On.Before(rows[j].On) })
	var usable []obsRow
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
	var verified []point
	var verifiedDates []time.Time
	for _, r := range usable {
		value, ok := convertValue(r.Value, r.Unit, canonical, known)
		if !ok {
			out.Excluded = append(out.Excluded, excludedPoint{ObservationID: r.ID, ObservedAt: r.On.Format("2006-01-02"), Value: r.Value, Unit: r.Unit, Reason: "no_approved_conversion"})
			continue
		}
		p := trendPoint{ObservationID: r.ID, ObservedAt: r.On.Format("2006-01-02"), Value: value, Unit: canonical, OriginalValue: r.Value, OriginalUnit: r.Unit, Converted: r.Unit != canonical, MeasurementKind: measurementKind(r.SourceType), Verification: r.Status, Revision: r.Revision}
		p.Range, p.Flag = rangeFor(r, learned, known, sex, age)
		out.Points = append(out.Points, p)
		kinds[p.MeasurementKind] = true
		if r.Status == "VERIFIED" {
			verified = append(verified, point{Value: value, On: r.On})
			verifiedDates = append(verifiedDates, r.On)
		}
	}
	for k := range kinds {
		out.Kinds = append(out.Kinds, k)
	}
	sort.Strings(out.Kinds)
	out.Trend = analyzeTrend(verified)
	if len(verifiedDates) > 0 {
		last := verifiedDates[len(verifiedDates)-1]
		since := int(now.Sub(last).Hours() / 24)
		out.Cadence.DaysSinceLast = &since
		if gap, ok := measurementCadence(verifiedDates); ok {
			days := int(gap.Hours()/24 + 0.5)
			out.Cadence.TypicalGapDays = &days
			out.Cadence.Overdue = overdueForMeasurement(last, now, gap)
		}
	}
	return out
}

// rangeFor picks the range that applies to one reading: the lab's own printed range first, since
// it matches the method and the unit it was measured in, then an approved learned range.
func rangeFor(r obsRow, learned []learnedRange, known map[conversionKey]conversion, sex *string, age *int) (*rangeInfo, string) {
	if low, high, ok := parseReferenceRange(r.LabRange); ok {
		return &rangeInfo{Low: low, High: high, Unit: r.Unit, Origin: "lab_report", Source: "Printed on the report"}, flagValue(r.Value, low, high)
	}
	var usable []learnedRange
	for _, l := range learned {
		if _, ok := convertValue(r.Value, r.Unit, l.Unit, known); ok {
			usable = append(usable, l)
		}
	}
	if pick := pickLearnedRange(usable, sex, age); pick != nil {
		value, _ := convertValue(r.Value, r.Unit, pick.Unit, known)
		return &rangeInfo{Low: pick.Low, High: pick.High, Unit: pick.Unit, Origin: "approved_reference", Source: pick.Source}, flagValue(value, pick.Low, pick.High)
	}
	return nil, "unknown"
}

func (a *API) loadBiomarkerKnowledge(ctx context.Context, code string) (map[conversionKey]conversion, []learnedRange, error) {
	known := map[conversionKey]conversion{}
	rows, err := a.db.Pool.Query(ctx, `SELECT from_unit,to_unit,factor::float8,"offset"::float8 FROM unit_conversions WHERE biomarker_code=$1`, code)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var from, to string
		var c conversion
		if err := rows.Scan(&from, &to, &c.Factor, &c.Offset); err != nil {
			rows.Close()
			return nil, nil, err
		}
		known[conversionKey{from, to}] = c
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	ranges := []learnedRange{}
	rr, err := a.db.Pool.Query(ctx, `SELECT unit,sex,age_min,age_max,low::float8,high::float8,source FROM reference_ranges WHERE biomarker_code=$1 ORDER BY id`, code)
	if err != nil {
		return nil, nil, err
	}
	defer rr.Close()
	for rr.Next() {
		var l learnedRange
		if err := rr.Scan(&l.Unit, &l.Sex, &l.AgeMin, &l.AgeMax, &l.Low, &l.High, &l.Source); err != nil {
			return nil, nil, err
		}
		ranges = append(ranges, l)
	}
	return known, ranges, rr.Err()
}

func (a *API) loadObservationRows(ctx context.Context, ownerID, profileID string, code *string) (map[string][]obsRow, map[string]string, error) {
	rows, err := a.db.Pool.Query(ctx, `SELECT o.id::text,o.biomarker_code,COALESCE(c.display_name,o.name),o.value::float8,o.unit,COALESCE(o.reference_range,''),COALESCE(o.observed_at,o.created_at::date),o.source_type,o.verification_status,o.revision
		FROM observations o LEFT JOIN biomarker_catalog c ON c.code=o.biomarker_code
		WHERE o.profile_id=$1 AND o.owner_id=$2 AND o.biomarker_code IS NOT NULL AND ($3::text IS NULL OR o.biomarker_code=$3::text)`, profileID, ownerID, code)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byCode, names := map[string][]obsRow{}, map[string]string{}
	for rows.Next() {
		var r obsRow
		var c, name string
		if err := rows.Scan(&r.ID, &c, &name, &r.Value, &r.Unit, &r.LabRange, &r.On, &r.SourceType, &r.Status, &r.Revision); err != nil {
			return nil, nil, err
		}
		byCode[c] = append(byCode[c], r)
		names[c] = name
	}
	return byCode, names, rows.Err()
}

func (a *API) profileDemographics(ctx context.Context, ownerID, profileID string, now time.Time) (*string, *int) {
	var sex *string
	var birth *time.Time
	if err := a.db.Pool.QueryRow(ctx, `SELECT sex,date_of_birth FROM profiles WHERE id=$1 AND owner_id=$2`, profileID, ownerID).Scan(&sex, &birth); err != nil {
		return nil, nil
	}
	if sex != nil && *sex != "female" && *sex != "male" {
		sex = nil
	}
	if birth == nil {
		return sex, nil
	}
	age := ageYears(*birth, now)
	return sex, &age
}

func (a *API) biomarkerTrend(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID, code := chi.URLParam(r, "profileID"), strings.ToUpper(chi.URLParam(r, "code"))
	includeUnverified := r.URL.Query().Get("include_unverified") == "true"
	byCode, names, err := a.loadObservationRows(r.Context(), owner(r), profileID, &code)
	if err != nil {
		writeError(w, 500, "TREND_UNAVAILABLE", "The trend is unavailable.", nil)
		return
	}
	if len(byCode[code]) == 0 {
		var exists bool
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM biomarker_catalog WHERE code=$1)`, code).Scan(&exists)
		if !exists {
			writeError(w, 404, "BIOMARKER_NOT_FOUND", "The biomarker is not in the catalog.", nil)
			return
		}
		var display string
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT display_name FROM biomarker_catalog WHERE code=$1`, code).Scan(&display)
		names[code] = display
	}
	known, learned, err := a.loadBiomarkerKnowledge(r.Context(), code)
	if err != nil {
		writeError(w, 500, "TREND_UNAVAILABLE", "The trend is unavailable.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := a.profileDemographics(r.Context(), owner(r), profileID, now)
	writeJSON(w, 200, buildTrend(code, names[code], byCode[code], includeUnverified, known, learned, sex, age, now))
}

type biomarkerSummary struct {
	BiomarkerCode string      `json:"biomarker_code"`
	DisplayName   string      `json:"display_name"`
	Latest        *trendPoint `json:"latest"`
	Previous      *float64    `json:"previous_value"`
	Change        *float64    `json:"change"`
	Unit          string      `json:"unit"`
	Trend         trendResult `json:"trend"`
	Cadence       cadenceInfo `json:"cadence"`
	Readings      int         `json:"verified_readings"`
	Unverified    int         `json:"unverified_count"`
	Excluded      int         `json:"excluded_count"`
	Kinds         []string    `json:"measurement_kinds"`
}

func (a *API) biomarkerSummaries(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	byCode, names, err := a.loadObservationRows(r.Context(), owner(r), profileID, nil)
	if err != nil {
		writeError(w, 500, "SUMMARY_UNAVAILABLE", "The biomarker summary is unavailable.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := a.profileDemographics(r.Context(), owner(r), profileID, now)
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	out := []biomarkerSummary{}
	for _, code := range codes {
		known, learned, err := a.loadBiomarkerKnowledge(r.Context(), code)
		if err != nil {
			writeError(w, 500, "SUMMARY_UNAVAILABLE", "The biomarker summary is unavailable.", nil)
			return
		}
		t := buildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		s := biomarkerSummary{BiomarkerCode: code, DisplayName: t.DisplayName, Unit: t.CanonicalUnit, Trend: t.Trend, Cadence: t.Cadence, Readings: len(t.Points), Unverified: t.Unverified, Excluded: len(t.Excluded), Kinds: t.Kinds}
		if n := len(t.Points); n > 0 {
			latest := t.Points[n-1]
			s.Latest = &latest
			if n > 1 {
				prev := t.Points[n-2].Value
				change := latest.Value - prev
				s.Previous, s.Change = &prev, &change
			}
		}
		out = append(out, s)
	}
	writeJSON(w, 200, out)
}
