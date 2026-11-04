package lifestyle

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxSymptomsPerProfile = 5000

const journalNote = "This journal is self-reported and descriptive. It is not a diagnosis; talk to a clinician about symptoms that worry you."

type symptomRecord struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Severity  int        `json:"severity"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Notes     *string    `json:"notes,omitempty"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
}

const symptomColumns = `id::text,name,severity,started_at,ended_at,notes,ended_at IS NULL,created_at`

func scanSymptom(row interface{ Scan(...any) error }) (symptomRecord, error) {
	var s symptomRecord
	err := row.Scan(&s.ID, &s.Name, &s.Severity, &s.StartedAt, &s.EndedAt, &s.Notes, &s.Active, &s.CreatedAt)
	return s, err
}

type symptomInput struct {
	Name      *string    `json:"name"`
	Severity  *int       `json:"severity"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Notes     *string    `json:"notes"`
}

func cleanNotes(v *string) (*string, bool) {
	if v == nil {
		return nil, true
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, true
	}
	return &t, len([]rune(t)) <= 1000
}

func (m *Module) categories(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, categoryList())
}

func (m *Module) addSymptom(w http.ResponseWriter, r *http.Request) {
	if !m.CanStore(w, r) {
		return
	}
	var in symptomInput
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || in.Name == nil || in.Severity == nil {
		httpx.Invalid(w, "INVALID_SYMPTOM", "The symptom details are invalid.", "body")
		return
	}
	name := strings.TrimSpace(*in.Name)
	notes, notesOK := cleanNotes(in.Notes)
	started := time.Now().UTC()
	if in.StartedAt != nil {
		started = in.StartedAt.UTC()
	}
	switch {
	case !httpx.ValidText(name, 120, true):
		httpx.Invalid(w, "INVALID_SYMPTOM", "A symptom name of up to 120 characters is required.", "name")
		return
	case *in.Severity < 1 || *in.Severity > 10:
		httpx.Invalid(w, "INVALID_SEVERITY", "Severity must be between 1 and 10.", "severity")
		return
	case !notesOK:
		httpx.Invalid(w, "INVALID_SYMPTOM", "Notes can be up to 1000 characters.", "notes")
		return
	case started.After(time.Now().Add(24 * time.Hour)):
		httpx.Invalid(w, "INVALID_DATE", "A symptom cannot start in the future.", "started_at")
		return
	case in.EndedAt != nil && in.EndedAt.Before(started):
		httpx.Invalid(w, "INVALID_DATE", "A symptom cannot end before it starts.", "ended_at")
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	var count int
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2`, profileID, owner).Scan(&count); err != nil {
		httpx.WriteError(w, 500, "SYMPTOM_SAVE_FAILED", "The symptom could not be saved.", nil)
		return
	}
	if count >= maxSymptomsPerProfile {
		httpx.WriteError(w, http.StatusConflict, "SYMPTOM_LIMIT_REACHED", "This profile has reached the symptom entry limit.", nil)
		return
	}
	s, err := scanSymptom(m.DB.Pool.QueryRow(r.Context(), `INSERT INTO symptom_entries(profile_id,owner_id,name,severity,started_at,ended_at,notes) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+symptomColumns,
		profileID, owner, name, *in.Severity, started, in.EndedAt, notes))
	if err != nil {
		httpx.WriteError(w, 500, "SYMPTOM_SAVE_FAILED", "The symptom could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s)
}

func (m *Module) listSymptoms(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	from, e1 := httpx.ParseDay(httpx.OptionalQuery(r, "from"))
	to, e2 := httpx.ParseDay(httpx.OptionalQuery(r, "to"))
	if e1 != nil || e2 != nil {
		httpx.Invalid(w, "INVALID_DATE", "Use YYYY-MM-DD for from and to.", "from")
		return
	}
	q := r.URL.Query()
	active := q.Get("active")
	if active != "" && active != "true" && active != "false" {
		httpx.Invalid(w, "INVALID_FILTER", "active must be true or false.", "active")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+symptomColumns+` FROM symptom_entries
		WHERE profile_id=$1 AND owner_id=$2 AND ($3::text='' OR lower(name)=lower($3))
		  AND ($4::date IS NULL OR (started_at AT TIME ZONE 'UTC')::date>=$4::date)
		  AND ($5::date IS NULL OR (started_at AT TIME ZONE 'UTC')::date<=$5::date)
		  AND ($6::text='' OR (ended_at IS NULL)=($6='true'))
		ORDER BY started_at DESC,id LIMIT $7 OFFSET $8`,
		chi.URLParam(r, "profileID"), httpx.Owner(r), q.Get("name"), from, to, active, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "SYMPTOMS_UNAVAILABLE", "Symptoms could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []symptomRecord{}
	for rows.Next() {
		s, err := scanSymptom(rows)
		if err != nil {
			httpx.WriteError(w, 500, "SYMPTOMS_UNAVAILABLE", "Symptoms could not be loaded.", nil)
			return
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ownedSymptom resolves {symptomID} within the caller's profile, writing a 404 when it is not theirs.
func (m *Module) ownedSymptom(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "symptomID")
	var found bool
	if _, err := uuid.Parse(id); err == nil {
		_ = m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM symptom_entries WHERE id=$1 AND profile_id=$2 AND owner_id=$3)`, id, chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&found)
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "SYMPTOM_NOT_FOUND", "The requested symptom could not be found.", nil)
	}
	return id, found
}

// updateSymptom changes severity, notes or the end time, which is how a symptom is marked resolved.
func (m *Module) updateSymptom(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedSymptom(w, r)
	if !ok {
		return
	}
	var in symptomInput
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || (in.Severity == nil && in.EndedAt == nil && in.Notes == nil) {
		httpx.Invalid(w, "INVALID_SYMPTOM", "Provide severity, ended_at or notes.", "body")
		return
	}
	if in.Severity != nil && (*in.Severity < 1 || *in.Severity > 10) {
		httpx.Invalid(w, "INVALID_SEVERITY", "Severity must be between 1 and 10.", "severity")
		return
	}
	notes, notesOK := cleanNotes(in.Notes)
	if !notesOK {
		httpx.Invalid(w, "INVALID_SYMPTOM", "Notes can be up to 1000 characters.", "notes")
		return
	}
	var started time.Time
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT started_at FROM symptom_entries WHERE id=$1`, id).Scan(&started)
	if in.EndedAt != nil && in.EndedAt.Before(started) {
		httpx.Invalid(w, "INVALID_DATE", "A symptom cannot end before it starts.", "ended_at")
		return
	}
	s, err := scanSymptom(m.DB.Pool.QueryRow(r.Context(), `UPDATE symptom_entries SET severity=COALESCE($2,severity),ended_at=COALESCE($3,ended_at),
		notes=CASE WHEN $4::boolean THEN $5 ELSE notes END,updated_at=now() WHERE id=$1 RETURNING `+symptomColumns,
		id, in.Severity, in.EndedAt, in.Notes != nil, notes))
	if err != nil {
		httpx.WriteError(w, 500, "SYMPTOM_SAVE_FAILED", "The symptom could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (m *Module) deleteSymptom(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedSymptom(w, r)
	if !ok {
		return
	}
	if _, err := m.DB.Pool.Exec(r.Context(), `DELETE FROM symptom_entries WHERE id=$1`, id); err != nil {
		httpx.WriteError(w, 500, "SYMPTOM_DELETE_FAILED", "The symptom could not be deleted.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type symptomGroup struct {
	Name        string    `json:"name"`
	Count       int       `json:"count"`
	AvgSeverity float64   `json:"average_severity"`
	MaxSeverity int       `json:"max_severity"`
	LastAt      time.Time `json:"last_at"`
	Active      int       `json:"active"`
}

// symptomSummary groups the journal by symptom name over the last N days (default 90).
func (m *Module) symptomSummary(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	days, ok := windowDays(w, r)
	if !ok {
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT lower(name),count(*),round(avg(severity)::numeric,1)::float8,max(severity),max(started_at),count(*) FILTER (WHERE ended_at IS NULL)
		FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND started_at>=now()-make_interval(days=>$3)
		GROUP BY lower(name) ORDER BY max(started_at) DESC,lower(name)`, chi.URLParam(r, "profileID"), httpx.Owner(r), days)
	if err != nil {
		httpx.WriteError(w, 500, "SYMPTOMS_UNAVAILABLE", "Symptoms could not be loaded.", nil)
		return
	}
	defer rows.Close()
	groups := []symptomGroup{}
	for rows.Next() {
		var g symptomGroup
		if err := rows.Scan(&g.Name, &g.Count, &g.AvgSeverity, &g.MaxSeverity, &g.LastAt, &g.Active); err != nil {
			httpx.WriteError(w, 500, "SYMPTOMS_UNAVAILABLE", "Symptoms could not be loaded.", nil)
			return
		}
		groups = append(groups, g)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"days": days, "symptoms": groups, "note": journalNote})
}
