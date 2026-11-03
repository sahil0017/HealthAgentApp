package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var (
	historyKinds  = []string{"condition", "allergy", "surgery", "family_history", "medication", "vaccination"}
	followUpKinds = []string{"investigation", "appointment", "medication", "other"}
)

func parseDay(value *string) (*string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	if err != nil {
		return nil, err
	}
	day := parsed.Format("2006-01-02")
	return &day, nil
}

// canStore reports whether the profile has agreed to store health data, which every write here needs.
func (a *API) canStore(w http.ResponseWriter, r *http.Request) bool {
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return false
	}
	profileID := chi.URLParam(r, "profileID")
	if !a.hasConsent(r.Context(), profileID, "medical_record_storage") && !a.hasConsent(r.Context(), profileID, "health_data") {
		writeError(w, 403, "CONSENT_REQUIRED", "Storing health information requires your consent.", nil)
		return false
	}
	return true
}

func (a *API) profileReadable(w http.ResponseWriter, r *http.Request) bool {
	if !a.profileOwned(r) {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return false
	}
	return true
}

func invalid(w http.ResponseWriter, code, message, field string) {
	writeError(w, 400, code, message, map[string]string{field: "invalid"})
}

func validText(value string, max int, required bool) bool {
	length := len([]rune(value))
	return length <= max && (!required || length > 0)
}

// ---- medical history ----

type historyEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Notes     string    `json:"notes"`
	StartedOn *string   `json:"started_on"`
	EndedOn   *string   `json:"ended_on"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

const historyColumns = `id::text,kind,title,notes,started_on::text,ended_on::text,status,created_at`

func scanHistory(row pgx.Row) (historyEntry, error) {
	var e historyEntry
	err := row.Scan(&e.ID, &e.Kind, &e.Title, &e.Notes, &e.StartedOn, &e.EndedOn, &e.Status, &e.CreatedAt)
	return e, err
}

func (a *API) listHistory(w http.ResponseWriter, r *http.Request) {
	if !a.profileReadable(w, r) {
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT `+historyColumns+` FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR kind=$3) ORDER BY started_on DESC NULLS LAST, created_at DESC`, chi.URLParam(r, "profileID"), owner(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, 500, "HISTORY_FAILED", "Medical history could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []historyEntry{}
	for rows.Next() {
		e, err := scanHistory(rows)
		if err != nil {
			writeError(w, 500, "HISTORY_FAILED", "Medical history could not be loaded.", nil)
			return
		}
		items = append(items, e)
	}
	writeJSON(w, 200, items)
}

type historyInput struct {
	Kind      *string `json:"kind"`
	Title     *string `json:"title"`
	Notes     *string `json:"notes"`
	StartedOn *string `json:"started_on"`
	EndedOn   *string `json:"ended_on"`
	Status    *string `json:"status"`
}

func (in *historyInput) validate(creating bool) (string, string) {
	if creating && (in.Kind == nil || in.Title == nil) {
		return "kind", "kind and title are required"
	}
	if in.Kind != nil && !contains(historyKinds, *in.Kind) {
		return "kind", "unsupported kind"
	}
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		in.Title = &trimmed
		if !validText(trimmed, 200, true) {
			return "title", "title must be 1 to 200 characters"
		}
	}
	if in.Notes != nil && !validText(*in.Notes, 2000, false) {
		return "notes", "notes are too long"
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "resolved" {
		return "status", "status must be active or resolved"
	}
	return "", ""
}

func (a *API) createHistory(w http.ResponseWriter, r *http.Request) {
	var in historyInput
	if decodeJSON(r, 16<<10, &in) != nil {
		invalid(w, "INVALID_HISTORY", "The history entry is not valid.", "body")
		return
	}
	if field, msg := in.validate(true); field != "" {
		invalid(w, "INVALID_HISTORY", msg, field)
		return
	}
	started, err1 := parseDay(in.StartedOn)
	ended, err2 := parseDay(in.EndedOn)
	if err1 != nil || err2 != nil || (started != nil && ended != nil && *ended < *started) {
		invalid(w, "INVALID_HISTORY", "Dates must be YYYY-MM-DD and the end cannot precede the start.", "started_on")
		return
	}
	if !a.canStore(w, r) {
		return
	}
	notes, status := "", "active"
	if in.Notes != nil {
		notes = *in.Notes
	}
	if in.Status != nil {
		status = *in.Status
	}
	profileID := chi.URLParam(r, "profileID")
	e, err := scanHistory(a.db.Pool.QueryRow(r.Context(), `INSERT INTO history_entries(owner_id,profile_id,kind,title,notes,started_on,ended_on,status) VALUES($1,$2,$3,$4,$5,$6::text::date,$7::text::date,$8) RETURNING `+historyColumns, owner(r), profileID, *in.Kind, *in.Title, notes, started, ended, status))
	if err != nil {
		writeError(w, 500, "HISTORY_FAILED", "The history entry could not be saved.", nil)
		return
	}
	a.audit(r, profileID, "history.created", "history_entry", e.ID)
	writeJSON(w, 201, e)
}

func (a *API) updateHistory(w http.ResponseWriter, r *http.Request) {
	var in historyInput
	if decodeJSON(r, 16<<10, &in) != nil {
		invalid(w, "INVALID_HISTORY", "The history entry is not valid.", "body")
		return
	}
	if field, msg := in.validate(false); field != "" {
		invalid(w, "INVALID_HISTORY", msg, field)
		return
	}
	started, err1 := parseDay(in.StartedOn)
	ended, err2 := parseDay(in.EndedOn)
	if err1 != nil || err2 != nil {
		invalid(w, "INVALID_HISTORY", "Dates must be YYYY-MM-DD.", "started_on")
		return
	}
	e, err := scanHistory(a.db.Pool.QueryRow(r.Context(), `UPDATE history_entries SET kind=COALESCE($3,kind),title=COALESCE($4,title),notes=COALESCE($5,notes),started_on=COALESCE($6::text::date,started_on),ended_on=COALESCE($7::text::date,ended_on),status=COALESCE($8,status),updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING `+historyColumns, chi.URLParam(r, "entryID"), owner(r), in.Kind, in.Title, in.Notes, started, ended, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "HISTORY_NOT_FOUND", "The history entry could not be found.", nil)
		return
	}
	if err != nil {
		invalid(w, "INVALID_HISTORY", "The end date cannot precede the start date.", "ended_on")
		return
	}
	a.audit(r, "", "history.updated", "history_entry", e.ID)
	writeJSON(w, 200, e)
}

func (a *API) deleteHistory(w http.ResponseWriter, r *http.Request) {
	a.deleteOwned(w, r, "history_entries", chi.URLParam(r, "entryID"), "HISTORY_NOT_FOUND", "history.deleted", "history_entry")
}

// ---- follow-ups and reminders ----

type followUp struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	DueOn       *string    `json:"due_on"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

const followUpColumns = `id::text,kind,title,notes,due_on::text,status,completed_at,created_at`

func scanFollowUp(row pgx.Row) (followUp, error) {
	var f followUp
	err := row.Scan(&f.ID, &f.Kind, &f.Title, &f.Notes, &f.DueOn, &f.Status, &f.CompletedAt, &f.CreatedAt)
	return f, err
}

func (a *API) listFollowUps(w http.ResponseWriter, r *http.Request) {
	if !a.profileReadable(w, r) {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "open" && status != "done" && status != "dismissed" {
		invalid(w, "INVALID_FOLLOW_UP", "status must be open, done or dismissed.", "status")
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT `+followUpColumns+` FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR status=$3) ORDER BY (status<>'open'), due_on NULLS LAST, created_at DESC`, chi.URLParam(r, "profileID"), owner(r), status)
	if err != nil {
		writeError(w, 500, "FOLLOW_UP_FAILED", "Follow-ups could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []followUp{}
	for rows.Next() {
		f, err := scanFollowUp(rows)
		if err != nil {
			writeError(w, 500, "FOLLOW_UP_FAILED", "Follow-ups could not be loaded.", nil)
			return
		}
		items = append(items, f)
	}
	writeJSON(w, 200, items)
}

type followUpInput struct {
	Kind   *string `json:"kind"`
	Title  *string `json:"title"`
	Notes  *string `json:"notes"`
	DueOn  *string `json:"due_on"`
	Status *string `json:"status"`
}

func (in *followUpInput) validate(creating bool) (string, string) {
	if creating && (in.Kind == nil || in.Title == nil) {
		return "kind", "kind and title are required"
	}
	if in.Kind != nil && !contains(followUpKinds, *in.Kind) {
		return "kind", "unsupported kind"
	}
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		in.Title = &trimmed
		if !validText(trimmed, 200, true) {
			return "title", "title must be 1 to 200 characters"
		}
	}
	if in.Notes != nil && !validText(*in.Notes, 2000, false) {
		return "notes", "notes are too long"
	}
	if in.Status != nil && !contains([]string{"open", "done", "dismissed"}, *in.Status) {
		return "status", "status must be open, done or dismissed"
	}
	return "", ""
}

func (a *API) createFollowUp(w http.ResponseWriter, r *http.Request) {
	var in followUpInput
	if decodeJSON(r, 16<<10, &in) != nil {
		invalid(w, "INVALID_FOLLOW_UP", "The follow-up is not valid.", "body")
		return
	}
	if field, msg := in.validate(true); field != "" {
		invalid(w, "INVALID_FOLLOW_UP", msg, field)
		return
	}
	due, err := parseDay(in.DueOn)
	if err != nil {
		invalid(w, "INVALID_FOLLOW_UP", "due_on must be YYYY-MM-DD.", "due_on")
		return
	}
	if !a.canStore(w, r) {
		return
	}
	notes := ""
	if in.Notes != nil {
		notes = *in.Notes
	}
	profileID := chi.URLParam(r, "profileID")
	f, err := scanFollowUp(a.db.Pool.QueryRow(r.Context(), `INSERT INTO follow_ups(owner_id,profile_id,kind,title,notes,due_on) VALUES($1,$2,$3,$4,$5,$6::text::date) RETURNING `+followUpColumns, owner(r), profileID, *in.Kind, *in.Title, notes, due))
	if err != nil {
		writeError(w, 500, "FOLLOW_UP_FAILED", "The follow-up could not be saved.", nil)
		return
	}
	a.audit(r, profileID, "follow_up.created", "follow_up", f.ID)
	writeJSON(w, 201, f)
}

func (a *API) updateFollowUp(w http.ResponseWriter, r *http.Request) {
	var in followUpInput
	if decodeJSON(r, 16<<10, &in) != nil {
		invalid(w, "INVALID_FOLLOW_UP", "The follow-up is not valid.", "body")
		return
	}
	if field, msg := in.validate(false); field != "" {
		invalid(w, "INVALID_FOLLOW_UP", msg, field)
		return
	}
	due, err := parseDay(in.DueOn)
	if err != nil {
		invalid(w, "INVALID_FOLLOW_UP", "due_on must be YYYY-MM-DD.", "due_on")
		return
	}
	f, err := scanFollowUp(a.db.Pool.QueryRow(r.Context(), `UPDATE follow_ups SET kind=COALESCE($3,kind),title=COALESCE($4,title),notes=COALESCE($5,notes),due_on=COALESCE($6::text::date,due_on),status=COALESCE($7,status),completed_at=CASE WHEN COALESCE($7,status)='open' THEN NULL WHEN $7 IS NOT NULL AND $7<>status THEN now() ELSE completed_at END,updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING `+followUpColumns, chi.URLParam(r, "followUpID"), owner(r), in.Kind, in.Title, in.Notes, due, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "FOLLOW_UP_NOT_FOUND", "The follow-up could not be found.", nil)
		return
	}
	if err != nil {
		writeError(w, 500, "FOLLOW_UP_FAILED", "The follow-up could not be saved.", nil)
		return
	}
	a.audit(r, "", "follow_up.updated", "follow_up", f.ID)
	writeJSON(w, 200, f)
}

func (a *API) deleteFollowUp(w http.ResponseWriter, r *http.Request) {
	a.deleteOwned(w, r, "follow_ups", chi.URLParam(r, "followUpID"), "FOLLOW_UP_NOT_FOUND", "follow_up.deleted", "follow_up")
}

// ---- shared ----

// deleteOwned removes one owner-scoped row. The table name is always a constant from this package.
func (a *API) deleteOwned(w http.ResponseWriter, r *http.Request, table, id, notFound, action, resource string) {
	var profileID string
	err := a.db.Pool.QueryRow(r.Context(), `DELETE FROM `+table+` WHERE id=$1 AND owner_id=$2 RETURNING profile_id::text`, id, owner(r)).Scan(&profileID)
	if err != nil {
		writeError(w, 404, notFound, "The item could not be found.", nil)
		return
	}
	a.audit(r, profileID, action, resource, id)
	w.WriteHeader(http.StatusNoContent)
}

// audit records an action. When profileID is empty it is resolved from the resource's own row where possible.
func (a *API) audit(r *http.Request, profileID, action, resource, id string) {
	if profileID == "" {
		profileID = a.profileOfResource(r, resource, id)
	}
	var pid any
	if profileID != "" {
		pid = profileID
	}
	_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2::text::uuid,$3,$4,$5)`, owner(r), pid, action, resource, id)
}

func (a *API) profileOfResource(r *http.Request, resource, id string) string {
	tables := map[string]string{"history_entry": "history_entries", "follow_up": "follow_ups", "goal": "goals"}
	table, ok := tables[resource]
	if !ok {
		return ""
	}
	var profileID string
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT profile_id::text FROM `+table+` WHERE id=$1 AND owner_id=$2`, id, owner(r)).Scan(&profileID)
	return profileID
}
