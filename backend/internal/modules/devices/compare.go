package devices

import (
	"net/http"
	"strconv"
	"time"

	"cortex/backend/internal/domain/device"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const comparisonNote = "A laboratory result and a device reading measure different things in different ways, so they are not interchangeable. The laboratory result is the reference; use the device values for day-to-day patterns."

type labSide struct {
	ObservationID string  `json:"observation_id"`
	DocumentID    string  `json:"document_id,omitempty"`
	Date          string  `json:"date"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit"`
}

type deviceSide struct {
	Count int     `json:"reading_count"`
	Mean  float64 `json:"mean"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

type comparisonPair struct {
	Lab           labSide     `json:"lab"`
	Device        *deviceSide `json:"device"`
	Difference    *float64    `json:"difference"`
	DifferencePct *float64    `json:"difference_pct"`
}

type excludedLab struct {
	ObservationID string `json:"observation_id"`
	Date          string `json:"date"`
	Reason        string `json:"reason"`
}

func round1(v float64) float64 { return float64(int64(v*10+sign(v)*0.5)) / 10 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// labComparison sets recent verified laboratory results beside the device readings taken within a few
// days of each, for metrics that have a laboratory equivalent. Neither side replaces the other.
func (m *Module) labComparison(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	metric, ok := device.Lookup(chi.URLParam(r, "metric"))
	if !ok {
		httpx.Invalid(w, "INVALID_METRIC", "That metric is not supported.", "metric")
		return
	}
	if metric.LabCode == "" {
		httpx.WriteError(w, http.StatusBadRequest, "NO_LAB_EQUIVALENT", "This metric has no laboratory equivalent to compare with.", nil)
		return
	}
	window := 3
	if raw := r.URL.Query().Get("window_days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 14 {
			httpx.Invalid(w, "INVALID_RANGE", "window_days must be between 0 and 14.", "window_days")
			return
		}
		window = n
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text,COALESCE(document_id::text,''),COALESCE(observed_at,created_at::date),value::float8,unit
		FROM observations WHERE profile_id=$1 AND owner_id=$2 AND biomarker_code=$3 AND source_type='LAB_DOCUMENT' AND verification_status='VERIFIED'
		ORDER BY COALESCE(observed_at,created_at::date) DESC LIMIT 10`, profileID, owner, metric.LabCode)
	if err != nil {
		httpx.WriteError(w, 500, "COMPARISON_UNAVAILABLE", "The comparison could not be loaded.", nil)
		return
	}
	type labRow struct {
		id, doc, unit string
		on            time.Time
		value         float64
	}
	var labs []labRow
	for rows.Next() {
		var l labRow
		if err := rows.Scan(&l.id, &l.doc, &l.on, &l.value, &l.unit); err != nil {
			rows.Close()
			httpx.WriteError(w, 500, "COMPARISON_UNAVAILABLE", "The comparison could not be loaded.", nil)
			return
		}
		labs = append(labs, l)
	}
	rows.Close()

	pairs, excluded := []comparisonPair{}, []excludedLab{}
	for _, l := range labs {
		date := l.on.Format("2006-01-02")
		value, convertible := device.ToUnit(metric.Key, l.value, l.unit)
		if !convertible {
			excluded = append(excluded, excludedLab{ObservationID: l.id, Date: date, Reason: "unit " + l.unit + " cannot be compared with " + metric.Unit})
			continue
		}
		pair := comparisonPair{Lab: labSide{ObservationID: l.id, DocumentID: l.doc, Date: date, Value: round1(value), Unit: metric.Unit}}
		var side deviceSide
		if err := m.DB.Pool.QueryRow(r.Context(), `SELECT count(*),COALESCE(avg(value),0),COALESCE(min(value),0),COALESCE(max(value),0) FROM device_readings
			WHERE profile_id=$1 AND owner_id=$2 AND metric=$3 AND local_date BETWEEN $4::date-$5::int AND $4::date+$5::int`, profileID, owner, metric.Key, l.on, window).
			Scan(&side.Count, &side.Mean, &side.Min, &side.Max); err != nil {
			httpx.WriteError(w, 500, "COMPARISON_UNAVAILABLE", "The comparison could not be loaded.", nil)
			return
		}
		if side.Count > 0 {
			side.Mean, side.Min, side.Max = round1(side.Mean), round1(side.Min), round1(side.Max)
			diff := round1(side.Mean - pair.Lab.Value)
			pair.Device, pair.Difference = &side, &diff
			if pair.Lab.Value != 0 {
				pct := round1(diff / pair.Lab.Value * 100)
				pair.DifferencePct = &pct
			}
		}
		pairs = append(pairs, pair)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"metric": metric.Key, "name": metric.Name, "unit": metric.Unit, "lab_biomarker_code": metric.LabCode, "window_days": window, "pairs": pairs, "excluded": excluded, "note": comparisonNote})
}
