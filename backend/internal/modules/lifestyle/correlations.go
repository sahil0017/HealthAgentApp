package lifestyle

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/domain/lifestyle"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const correlationNote = "These are associations between things you logged, not causes. A pattern can be coincidence, and a missing pattern does not rule anything out. Share notable patterns with a clinician."

type correlationRow struct {
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	R           *float64 `json:"r,omitempty"`
	Days        int      `json:"days"`
	Strength    string   `json:"strength,omitempty"`
	Description string   `json:"description"`
}

// correlations compares each lifestyle category with symptom severity. For every day the category was
// logged, severity is the worst matching symptom that began on day+lag (0 when none). lag=1 asks whether
// today's habits relate to tomorrow's symptoms.
func (m *Module) correlations(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	days, ok := windowDays(w, r)
	if !ok {
		return
	}
	lag := 0
	switch r.URL.Query().Get("lag") {
	case "", "0":
	case "1":
		lag = 1
	default:
		httpx.Invalid(w, "INVALID_LAG", "lag must be 0 or 1.", "lag")
		return
	}
	symptom := strings.TrimSpace(r.URL.Query().Get("symptom"))
	if !httpx.ValidText(symptom, 120, false) {
		httpx.Invalid(w, "INVALID_SYMPTOM", "The symptom name is too long.", "symptom")
		return
	}
	severity, err := m.dailySeverity(r, days+lag, symptom)
	if err != nil {
		httpx.WriteError(w, 500, "CORRELATIONS_UNAVAILABLE", "Correlations could not be calculated.", nil)
		return
	}
	values, err := m.dailyValues(r, days)
	if err != nil {
		httpx.WriteError(w, 500, "CORRELATIONS_UNAVAILABLE", "Correlations could not be calculated.", nil)
		return
	}
	out := []correlationRow{}
	for _, c := range lifestyle.Categories() {
		row := correlationRow{Category: c.Code, Name: c.Name}
		var x, y []float64
		for _, day := range lifestyle.SortedKeys(values[c.Code]) {
			t, _ := time.Parse("2006-01-02", day)
			x = append(x, values[c.Code][day])
			y = append(y, severity[t.AddDate(0, 0, lag).Format("2006-01-02")])
		}
		row.Days = len(x)
		if corr, ok := lifestyle.Pearson(x, y); ok {
			row.Status, row.R, row.Strength = "ok", &corr.R, corr.Strength
			row.Description = describe(c.Name, symptom, corr, lag)
		} else {
			row.Status = "insufficient_data"
			row.Description = "Not enough varied days yet; log at least 7 days with differing values."
		}
		out = append(out, row)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"days": days, "lag": lag, "symptom": symptom, "correlations": out, "note": correlationNote})
}

func describe(category, symptom string, c lifestyle.Correlation, lag int) string {
	target := "symptom severity"
	if symptom != "" {
		target = strings.ToLower(symptom) + " severity"
	}
	when := "on the same day"
	if lag == 1 {
		when = "the next day"
	}
	return "Across " + itoa(c.Days) + " days there is a " + c.Strength + " association between " + strings.ToLower(category) + " and " + target + " " + when + "."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// dailySeverity returns date -> highest severity of symptoms that began that day (optionally by name).
func (m *Module) dailySeverity(r *http.Request, days int, symptom string) (map[string]float64, error) {
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT to_char((started_at AT TIME ZONE 'UTC')::date,'YYYY-MM-DD'),max(severity) FROM symptom_entries
		WHERE profile_id=$1 AND owner_id=$2 AND ($3::text='' OR lower(name)=lower($3)) AND started_at>=now()-make_interval(days=>$4+1)
		GROUP BY 1`, chi.URLParam(r, "profileID"), httpx.Owner(r), symptom, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var day string
		var s int
		if err := rows.Scan(&day, &s); err != nil {
			return nil, err
		}
		out[day] = float64(s)
	}
	return out, rows.Err()
}
