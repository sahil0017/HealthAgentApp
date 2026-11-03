package devices

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"cortex/backend/internal/domain/device"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const readingNote = "Readings from consumer devices are descriptive and are not a diagnosis. Typical ranges are general adult references and vary by person; talk to a clinician about anything that worries you."

type readingRecord struct {
	ID              string    `json:"id"`
	DeviceID        string    `json:"device_id"`
	DeviceName      string    `json:"device_name"`
	Metric          string    `json:"metric"`
	Value           float64   `json:"value"`
	Unit            string    `json:"unit"`
	OriginalValue   float64   `json:"original_value"`
	OriginalUnit    string    `json:"original_unit"`
	MeasuredAt      time.Time `json:"measured_at"`
	MeasurementKind string    `json:"measurement_kind"`
}

// listReadings pages raw readings, newest first, filtered by metric, device and local date range.
func (m *Module) listReadings(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	q := r.URL.Query()
	if metric := q.Get("metric"); metric != "" {
		if _, ok := device.Lookup(metric); !ok {
			httpx.Invalid(w, "INVALID_METRIC", "That metric is not supported.", "metric")
			return
		}
	}
	from, e1 := httpx.ParseDay(httpx.OptionalQuery(r, "from"))
	to, e2 := httpx.ParseDay(httpx.OptionalQuery(r, "to"))
	if e1 != nil || e2 != nil {
		httpx.Invalid(w, "INVALID_DATE", "Use YYYY-MM-DD for from and to.", "from")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT r.id::text,r.device_id::text,d.name,r.metric,r.value,r.unit,r.original_value,r.original_unit,r.measured_at
		FROM device_readings r JOIN devices d ON d.id=r.device_id
		WHERE r.profile_id=$1 AND r.owner_id=$2
		  AND ($3::text='' OR r.metric=$3) AND ($4::text='' OR r.device_id::text=$4)
		  AND ($5::date IS NULL OR r.local_date>=$5::date) AND ($6::date IS NULL OR r.local_date<=$6::date)
		ORDER BY r.measured_at DESC,r.id LIMIT $7 OFFSET $8`,
		chi.URLParam(r, "profileID"), httpx.Owner(r), q.Get("metric"), q.Get("device_id"), from, to, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []readingRecord{}
	for rows.Next() {
		var x readingRecord
		if err := rows.Scan(&x.ID, &x.DeviceID, &x.DeviceName, &x.Metric, &x.Value, &x.Unit, &x.OriginalValue, &x.OriginalUnit, &x.MeasuredAt); err != nil {
			httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
			return
		}
		x.MeasurementKind = "device"
		out = append(out, x)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// dailyDays loads per-day aggregates for one metric over the last n days (local dates).
func (m *Module) dailyDays(r *http.Request, metric string, days int) ([]device.Day, error) {
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT local_date::text,avg(value),min(value),max(value),sum(value),count(*)
		FROM device_readings WHERE profile_id=$1 AND owner_id=$2 AND metric=$3 AND local_date>=(current_date - $4::int)
		GROUP BY local_date ORDER BY local_date`, chi.URLParam(r, "profileID"), httpx.Owner(r), metric, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []device.Day{}
	for rows.Next() {
		var d device.Day
		if err := rows.Scan(&d.Date, &d.Mean, &d.Min, &d.Max, &d.Sum, &d.Count); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// dailySeries returns one metric as a series of daily aggregates.
func (m *Module) dailySeries(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	metric, ok := device.Lookup(chi.URLParam(r, "metric"))
	if !ok {
		httpx.Invalid(w, "INVALID_METRIC", "That metric is not supported.", "metric")
		return
	}
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 365 {
			httpx.Invalid(w, "INVALID_RANGE", "days must be between 1 and 365.", "days")
			return
		}
		days = n
	}
	series, err := m.dailyDays(r, metric.Key, days)
	if err != nil {
		httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"metric": metric.Key, "name": metric.Name, "unit": metric.Unit, "cumulative": metric.Sum, "days": series, "note": readingNote})
}

// readingSummary gives one descriptive summary per metric that has data: latest value, 7 and 30 day
// averages, and the change against the previous 30 days.
func (m *Module) readingSummary(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT metric,local_date::text,avg(value),min(value),max(value),sum(value),count(*)
		FROM device_readings WHERE profile_id=$1 AND owner_id=$2 AND local_date>=(current_date - 60)
		GROUP BY metric,local_date`, profileID, owner)
	if err != nil {
		httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
		return
	}
	byMetric := map[string][]device.Day{}
	for rows.Next() {
		var metric string
		var d device.Day
		if err := rows.Scan(&metric, &d.Date, &d.Mean, &d.Min, &d.Max, &d.Sum, &d.Count); err != nil {
			rows.Close()
			httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
			return
		}
		byMetric[metric] = append(byMetric[metric], d)
	}
	rows.Close()

	latest, err := m.DB.Pool.Query(r.Context(), `SELECT DISTINCT ON (r.metric) r.metric,r.value,r.measured_at,d.name
		FROM device_readings r JOIN devices d ON d.id=r.device_id
		WHERE r.profile_id=$1 AND r.owner_id=$2 ORDER BY r.metric,r.measured_at DESC`, profileID, owner)
	if err != nil {
		httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
		return
	}
	defer latest.Close()
	now := time.Now().UTC()
	out := []device.Summary{}
	for latest.Next() {
		var key, name string
		var value float64
		var at time.Time
		if err := latest.Scan(&key, &value, &at, &name); err != nil {
			httpx.WriteError(w, 500, "READINGS_UNAVAILABLE", "Readings could not be loaded.", nil)
			return
		}
		if metric, ok := device.Lookup(key); ok {
			out = append(out, device.Summarize(metric, byMetric[key], value, at, name, now))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"summaries": out, "note": readingNote})
}
