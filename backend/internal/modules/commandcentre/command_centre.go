package commandcentre

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var (
	historyKinds  = []string{"condition", "allergy", "surgery", "family_history", "medication", "vaccination"}
	followUpKinds = []string{"investigation", "appointment", "medication", "other"}
)

type HistoryEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Notes     string    `json:"notes"`
	StartedOn *string   `json:"started_on"`
	EndedOn   *string   `json:"ended_on"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

const historyColumns = `id::text,kind,title,notes,started_on::text,ended_on::text,status,source,created_at`

func scanHistory(row pgx.Row) (HistoryEntry, error) {
	var e HistoryEntry
	err := row.Scan(&e.ID, &e.Kind, &e.Title, &e.Notes, &e.StartedOn, &e.EndedOn, &e.Status, &e.Source, &e.CreatedAt)
	return e, err
}

func (m *Module) listHistory(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+historyColumns+` FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR kind=$3) ORDER BY started_on DESC NULLS LAST, created_at DESC`, chi.URLParam(r, "profileID"), httpx.Owner(r), r.URL.Query().Get("kind"))
	if err != nil {
		httpx.WriteError(w, 500, "HISTORY_FAILED", "Medical history could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []HistoryEntry{}
	for rows.Next() {
		e, err := scanHistory(rows)
		if err != nil {
			httpx.WriteError(w, 500, "HISTORY_FAILED", "Medical history could not be loaded.", nil)
			return
		}
		items = append(items, e)
	}
	httpx.WriteJSON(w, 200, items)
}

type HistoryInput struct {
	Kind      *string `json:"kind"`
	Title     *string `json:"title"`
	Notes     *string `json:"notes"`
	StartedOn *string `json:"started_on"`
	EndedOn   *string `json:"ended_on"`
	Status    *string `json:"status"`
}

func (in *HistoryInput) validate(creating bool) (string, string) {
	if creating && (in.Kind == nil || in.Title == nil) {
		return "kind", "kind and title are required"
	}
	if in.Kind != nil && !httpx.Contains(historyKinds, *in.Kind) {
		return "kind", "unsupported kind"
	}
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		in.Title = &trimmed
		if !httpx.ValidText(trimmed, 200, true) {
			return "title", "title must be 1 to 200 characters"
		}
	}
	if in.Notes != nil && !httpx.ValidText(*in.Notes, 2000, false) {
		return "notes", "notes are too long"
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "resolved" {
		return "status", "status must be active or resolved"
	}
	return "", ""
}

func (m *Module) createHistory(w http.ResponseWriter, r *http.Request) {
	var in HistoryInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.Invalid(w, "INVALID_HISTORY", "The history entry is not valid.", "body")
		return
	}
	if field, msg := in.validate(true); field != "" {
		httpx.Invalid(w, "INVALID_HISTORY", msg, field)
		return
	}
	started, err1 := httpx.ParseDay(in.StartedOn)
	ended, err2 := httpx.ParseDay(in.EndedOn)
	if err1 != nil || err2 != nil || (started != nil && ended != nil && *ended < *started) {
		httpx.Invalid(w, "INVALID_HISTORY", "Dates must be YYYY-MM-DD and the end cannot precede the start.", "started_on")
		return
	}
	if !m.CanStore(w, r) {
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
	e, err := scanHistory(m.DB.Pool.QueryRow(r.Context(), `INSERT INTO history_entries(owner_id,profile_id,kind,title,notes,started_on,ended_on,status) VALUES($1,$2,$3,$4,$5,$6::text::date,$7::text::date,$8) ON CONFLICT (profile_id, kind, (btrim(regexp_replace(lower(title),'[^a-z0-9]+',' ','g')))) DO UPDATE SET source='user', title=CASE WHEN history_entries.source='report' THEN EXCLUDED.title ELSE history_entries.title END, notes=CASE WHEN history_entries.source='report' THEN EXCLUDED.notes ELSE history_entries.notes END, started_on=CASE WHEN history_entries.source='report' THEN EXCLUDED.started_on ELSE history_entries.started_on END, ended_on=CASE WHEN history_entries.source='report' THEN EXCLUDED.ended_on ELSE history_entries.ended_on END, status=CASE WHEN history_entries.source='report' THEN EXCLUDED.status ELSE history_entries.status END, updated_at=now() RETURNING `+historyColumns, httpx.Owner(r), profileID, *in.Kind, *in.Title, notes, started, ended, status))
	if err != nil {
		httpx.WriteError(w, 500, "HISTORY_FAILED", "The history entry could not be saved.", nil)
		return
	}
	m.audit(r, profileID, "history.created", "history_entry", e.ID)
	httpx.WriteJSON(w, 201, e)
}

func (m *Module) updateHistory(w http.ResponseWriter, r *http.Request) {
	var in HistoryInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.Invalid(w, "INVALID_HISTORY", "The history entry is not valid.", "body")
		return
	}
	if field, msg := in.validate(false); field != "" {
		httpx.Invalid(w, "INVALID_HISTORY", msg, field)
		return
	}
	started, err1 := httpx.ParseDay(in.StartedOn)
	ended, err2 := httpx.ParseDay(in.EndedOn)
	if err1 != nil || err2 != nil {
		httpx.Invalid(w, "INVALID_HISTORY", "Dates must be YYYY-MM-DD.", "started_on")
		return
	}
	e, err := scanHistory(m.DB.Pool.QueryRow(r.Context(), `UPDATE history_entries SET kind=COALESCE($3,kind),title=COALESCE($4,title),notes=COALESCE($5,notes),started_on=COALESCE($6::text::date,started_on),ended_on=COALESCE($7::text::date,ended_on),status=COALESCE($8,status),updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING `+historyColumns, chi.URLParam(r, "entryID"), httpx.Owner(r), in.Kind, in.Title, in.Notes, started, ended, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, 404, "HISTORY_NOT_FOUND", "The history entry could not be found.", nil)
		return
	}
	if err != nil {
		httpx.Invalid(w, "INVALID_HISTORY", "The end date cannot precede the start date.", "ended_on")
		return
	}
	m.audit(r, "", "history.updated", "history_entry", e.ID)
	httpx.WriteJSON(w, 200, e)
}

func (m *Module) deleteHistory(w http.ResponseWriter, r *http.Request) {
	m.deleteOwned(w, r, "history_entries", chi.URLParam(r, "entryID"), "HISTORY_NOT_FOUND", "history.deleted", "history_entry")
}

type FollowUp struct {
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

func scanFollowUp(row pgx.Row) (FollowUp, error) {
	var f FollowUp
	err := row.Scan(&f.ID, &f.Kind, &f.Title, &f.Notes, &f.DueOn, &f.Status, &f.CompletedAt, &f.CreatedAt)
	return f, err
}

func (m *Module) listFollowUps(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "open" && status != "done" && status != "dismissed" {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", "status must be open, done or dismissed.", "status")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+followUpColumns+` FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR status=$3) ORDER BY (status<>'open'), due_on NULLS LAST, created_at DESC`, chi.URLParam(r, "profileID"), httpx.Owner(r), status)
	if err != nil {
		httpx.WriteError(w, 500, "FOLLOW_UP_FAILED", "Follow-ups could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []FollowUp{}
	for rows.Next() {
		f, err := scanFollowUp(rows)
		if err != nil {
			httpx.WriteError(w, 500, "FOLLOW_UP_FAILED", "Follow-ups could not be loaded.", nil)
			return
		}
		items = append(items, f)
	}
	httpx.WriteJSON(w, 200, items)
}

type FollowUpInput struct {
	Kind   *string `json:"kind"`
	Title  *string `json:"title"`
	Notes  *string `json:"notes"`
	DueOn  *string `json:"due_on"`
	Status *string `json:"status"`
}

func (in *FollowUpInput) validate(creating bool) (string, string) {
	if creating && (in.Kind == nil || in.Title == nil) {
		return "kind", "kind and title are required"
	}
	if in.Kind != nil && !httpx.Contains(followUpKinds, *in.Kind) {
		return "kind", "unsupported kind"
	}
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		in.Title = &trimmed
		if !httpx.ValidText(trimmed, 200, true) {
			return "title", "title must be 1 to 200 characters"
		}
	}
	if in.Notes != nil && !httpx.ValidText(*in.Notes, 2000, false) {
		return "notes", "notes are too long"
	}
	if in.Status != nil && !httpx.Contains([]string{"open", "done", "dismissed"}, *in.Status) {
		return "status", "status must be open, done or dismissed"
	}
	return "", ""
}

func (m *Module) createFollowUp(w http.ResponseWriter, r *http.Request) {
	var in FollowUpInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", "The follow-up is not valid.", "body")
		return
	}
	if field, msg := in.validate(true); field != "" {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", msg, field)
		return
	}
	due, err := httpx.ParseDay(in.DueOn)
	if err != nil {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", "due_on must be YYYY-MM-DD.", "due_on")
		return
	}
	if !m.CanStore(w, r) {
		return
	}
	notes := ""
	if in.Notes != nil {
		notes = *in.Notes
	}
	profileID := chi.URLParam(r, "profileID")
	f, err := scanFollowUp(m.DB.Pool.QueryRow(r.Context(), `INSERT INTO follow_ups(owner_id,profile_id,kind,title,notes,due_on) VALUES($1,$2,$3,$4,$5,$6::text::date) RETURNING `+followUpColumns, httpx.Owner(r), profileID, *in.Kind, *in.Title, notes, due))
	if err != nil {
		httpx.WriteError(w, 500, "FOLLOW_UP_FAILED", "The follow-up could not be saved.", nil)
		return
	}
	m.audit(r, profileID, "follow_up.created", "follow_up", f.ID)
	httpx.WriteJSON(w, 201, f)
}

func (m *Module) updateFollowUp(w http.ResponseWriter, r *http.Request) {
	var in FollowUpInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", "The follow-up is not valid.", "body")
		return
	}
	if field, msg := in.validate(false); field != "" {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", msg, field)
		return
	}
	due, err := httpx.ParseDay(in.DueOn)
	if err != nil {
		httpx.Invalid(w, "INVALID_FOLLOW_UP", "due_on must be YYYY-MM-DD.", "due_on")
		return
	}
	f, err := scanFollowUp(m.DB.Pool.QueryRow(r.Context(), `UPDATE follow_ups SET kind=COALESCE($3,kind),title=COALESCE($4,title),notes=COALESCE($5,notes),due_on=COALESCE($6::text::date,due_on),status=COALESCE($7,status),completed_at=CASE WHEN COALESCE($7,status)='open' THEN NULL WHEN $7 IS NOT NULL AND $7<>status THEN now() ELSE completed_at END,updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING `+followUpColumns, chi.URLParam(r, "followUpID"), httpx.Owner(r), in.Kind, in.Title, in.Notes, due, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, 404, "FOLLOW_UP_NOT_FOUND", "The follow-up could not be found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "FOLLOW_UP_FAILED", "The follow-up could not be saved.", nil)
		return
	}
	m.audit(r, "", "follow_up.updated", "follow_up", f.ID)
	httpx.WriteJSON(w, 200, f)
}

func (m *Module) deleteFollowUp(w http.ResponseWriter, r *http.Request) {
	m.deleteOwned(w, r, "follow_ups", chi.URLParam(r, "followUpID"), "FOLLOW_UP_NOT_FOUND", "follow_up.deleted", "follow_up")
}

// deleteOwned removes one owner-scoped row. The table name is always a constant from this package.
func (m *Module) deleteOwned(w http.ResponseWriter, r *http.Request, table, id, notFound, action, resource string) {
	var profileID string
	err := m.DB.Pool.QueryRow(r.Context(), `DELETE FROM `+table+` WHERE id=$1 AND owner_id=$2 RETURNING profile_id::text`, id, httpx.Owner(r)).Scan(&profileID)
	if err != nil {
		httpx.WriteError(w, 404, notFound, "The item could not be found.", nil)
		return
	}
	m.audit(r, profileID, action, resource, id)
	w.WriteHeader(http.StatusNoContent)
}

// audit records an action. When profileID is empty it is resolved from the resource's own row where possible.
func (m *Module) audit(r *http.Request, profileID, action, resource, id string) {
	if profileID == "" {
		profileID = m.profileOfResource(r, resource, id)
	}
	var pid any
	if profileID != "" {
		pid = profileID
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2::text::uuid,$3,$4,$5)`, httpx.Owner(r), pid, action, resource, id)
}

func (m *Module) profileOfResource(r *http.Request, resource, id string) string {
	tables := map[string]string{"history_entry": "history_entries", "follow_up": "follow_ups", "goal": "goals"}
	table, ok := tables[resource]
	if !ok {
		return ""
	}
	var profileID string
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT profile_id::text FROM `+table+` WHERE id=$1 AND owner_id=$2`, id, httpx.Owner(r)).Scan(&profileID)
	return profileID
}
