package lifestyle

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/domain/lifestyle"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxLogsPerProfile = 20000

func categoryList() []lifestyle.Category { return lifestyle.Categories() }

type logRecord struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Label     *string   `json:"label,omitempty"`
	Notes     *string   `json:"notes,omitempty"`
	LoggedOn  string    `json:"logged_on"`
	CreatedAt time.Time `json:"created_at"`
}

const logColumns = `id::text,category,value,unit,label,notes,to_char(logged_on,'YYYY-MM-DD'),created_at`

func scanLog(row interface{ Scan(...any) error }) (logRecord, error) {
	var l logRecord
	err := row.Scan(&l.ID, &l.Category, &l.Value, &l.Unit, &l.Label, &l.Notes, &l.LoggedOn, &l.CreatedAt)
	return l, err
}

type logInput struct {
	Category string   `json:"category"`
	Value    *float64 `json:"value"`
	Label    *string  `json:"label"`
	Notes    *string  `json:"notes"`
	LoggedOn *string  `json:"logged_on"`
}

func (m *Module) addLog(w http.ResponseWriter, r *http.Request) {
	if !m.CanStore(w, r) {
		return
	}
	var in logInput
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || in.Value == nil {
		httpx.Invalid(w, "INVALID_LOG", "The lifestyle entry is invalid.", "body")
		return
	}
	cat, ok := lifestyle.Lookup(in.Category)
	if !ok {
		httpx.Invalid(w, "INVALID_CATEGORY", "That lifestyle category is not supported.", "category")
		return
	}
	if !cat.ValidValue(*in.Value) {
		httpx.Invalid(w, "INVALID_VALUE", "The value is outside the allowed range for "+cat.Code+".", "value")
		return
	}
	day, err := httpx.ParseDay(in.LoggedOn)
	if err != nil {
		httpx.Invalid(w, "INVALID_DATE", "Use YYYY-MM-DD for logged_on.", "logged_on")
		return
	}
	today := time.Now().UTC().Format("2006-01-02")
	if day == nil {
		day = &today
	} else if *day > time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02") {
		httpx.Invalid(w, "INVALID_DATE", "Entries cannot be dated in the future.", "logged_on")
		return
	}
	label, labelOK := optionalLabel(in.Label)
	notes, notesOK := cleanNotes(in.Notes)
	if !labelOK || !notesOK {
		httpx.Invalid(w, "INVALID_LOG", "Label up to 120 and notes up to 1000 characters.", "label")
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	var count int
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM lifestyle_logs WHERE profile_id=$1 AND owner_id=$2`, profileID, owner).Scan(&count); err != nil {
		httpx.WriteError(w, 500, "LOG_SAVE_FAILED", "The entry could not be saved.", nil)
		return
	}
	if count >= maxLogsPerProfile {
		httpx.WriteError(w, http.StatusConflict, "LOG_LIMIT_REACHED", "This profile has reached the lifestyle entry limit.", nil)
		return
	}
	l, err := scanLog(m.DB.Pool.QueryRow(r.Context(), `INSERT INTO lifestyle_logs(profile_id,owner_id,category,value,unit,label,notes,logged_on) VALUES($1,$2,$3,$4,$5,$6,$7,$8::date) RETURNING `+logColumns,
		profileID, owner, cat.Code, *in.Value, cat.Unit, label, notes, *day))
	if err != nil {
		httpx.WriteError(w, 500, "LOG_SAVE_FAILED", "The entry could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, l)
}

func optionalLabel(v *string) (*string, bool) {
	if v == nil {
		return nil, true
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, true
	}
	return &t, len([]rune(t)) <= 120
}

func (m *Module) listLogs(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	category := r.URL.Query().Get("category")
	if category != "" {
		if _, ok := lifestyle.Lookup(category); !ok {
			httpx.Invalid(w, "INVALID_CATEGORY", "That lifestyle category is not supported.", "category")
			return
		}
	}
	from, e1 := httpx.ParseDay(httpx.OptionalQuery(r, "from"))
	to, e2 := httpx.ParseDay(httpx.OptionalQuery(r, "to"))
	if e1 != nil || e2 != nil {
		httpx.Invalid(w, "INVALID_DATE", "Use YYYY-MM-DD for from and to.", "from")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+logColumns+` FROM lifestyle_logs WHERE profile_id=$1 AND owner_id=$2 AND ($3::text='' OR category=$3)
		AND ($4::date IS NULL OR logged_on>=$4::date) AND ($5::date IS NULL OR logged_on<=$5::date)
		ORDER BY logged_on DESC,created_at DESC,id LIMIT $6 OFFSET $7`, chi.URLParam(r, "profileID"), httpx.Owner(r), category, from, to, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "LOGS_UNAVAILABLE", "Entries could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []logRecord{}
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			httpx.WriteError(w, 500, "LOGS_UNAVAILABLE", "Entries could not be loaded.", nil)
			return
		}
		out = append(out, l)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (m *Module) deleteLog(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id := chi.URLParam(r, "logID")
	var deleted int64
	if _, err := uuid.Parse(id); err == nil {
		tag, derr := m.DB.Pool.Exec(r.Context(), `DELETE FROM lifestyle_logs WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, id, chi.URLParam(r, "profileID"), httpx.Owner(r))
		if derr != nil {
			httpx.WriteError(w, 500, "LOG_DELETE_FAILED", "The entry could not be deleted.", nil)
			return
		}
		deleted = tag.RowsAffected()
	}
	if deleted == 0 {
		httpx.WriteError(w, http.StatusNotFound, "LOG_NOT_FOUND", "The requested entry could not be found.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// windowDays reads ?days= (1-365, default 90).
func windowDays(w http.ResponseWriter, r *http.Request) (int, bool) {
	days := 90
	if raw := r.URL.Query().Get("days"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 365 {
			httpx.Invalid(w, "INVALID_WINDOW", "days must be between 1 and 365.", "days")
			return 0, false
		}
		days = v
	}
	return days, true
}

type dailyPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type categorySummary struct {
	Category   string       `json:"category"`
	Name       string       `json:"name"`
	Unit       string       `json:"unit"`
	DaysLogged int          `json:"days_logged"`
	Average    *float64     `json:"average_per_day"`
	Latest     *dailyPoint  `json:"latest"`
	Daily      []dailyPoint `json:"daily"`
}

// dailyValues loads each category's per-day combined value in the window, keyed category -> date.
func (m *Module) dailyValues(r *http.Request, days int) (map[string]map[string]float64, error) {
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT category,to_char(logged_on,'YYYY-MM-DD'),value FROM lifestyle_logs
		WHERE profile_id=$1 AND owner_id=$2 AND logged_on>=(now() AT TIME ZONE 'UTC')::date-$3::int ORDER BY logged_on`,
		chi.URLParam(r, "profileID"), httpx.Owner(r), days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	raw := map[string]map[string][]float64{}
	for rows.Next() {
		var cat, day string
		var v float64
		if err := rows.Scan(&cat, &day, &v); err != nil {
			return nil, err
		}
		if raw[cat] == nil {
			raw[cat] = map[string][]float64{}
		}
		raw[cat][day] = append(raw[cat][day], v)
	}
	out := map[string]map[string]float64{}
	for cat, byDay := range raw {
		c, _ := lifestyle.Lookup(cat)
		out[cat] = map[string]float64{}
		for day, values := range byDay {
			out[cat][day] = round1(c.Combine(values))
		}
	}
	return out, rows.Err()
}

func round1(v float64) float64 { return float64(int64(v*10+0.5*sign(v))) / 10 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// lifestyleSummary reports per-category daily series and averages over the last N days (default 90).
func (m *Module) lifestyleSummary(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	days, ok := windowDays(w, r)
	if !ok {
		return
	}
	values, err := m.dailyValues(r, days)
	if err != nil {
		httpx.WriteError(w, 500, "LOGS_UNAVAILABLE", "Entries could not be loaded.", nil)
		return
	}
	out := []categorySummary{}
	for _, c := range lifestyle.Categories() {
		s := categorySummary{Category: c.Code, Name: c.Name, Unit: c.Unit, Daily: []dailyPoint{}}
		byDay := values[c.Code]
		keys := lifestyle.SortedKeys(byDay)
		sort.Strings(keys)
		total := 0.0
		for _, day := range keys {
			s.Daily = append(s.Daily, dailyPoint{Date: day, Value: byDay[day]})
			total += byDay[day]
		}
		s.DaysLogged = len(keys)
		if len(keys) > 0 {
			avg := round1(total / float64(len(keys)))
			s.Average = &avg
			last := s.Daily[len(s.Daily)-1]
			s.Latest = &last
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"days": days, "categories": out, "note": journalNote})
}
