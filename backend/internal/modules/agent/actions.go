package agent

import (
	"errors"
	"net/http"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type action struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Reason    string     `json:"reason"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at"`
}

const actionColumns = `id::text,kind,title,reason,status,created_at,decided_at`

func scanAction(row pgx.Row) (action, error) {
	var a action
	err := row.Scan(&a.ID, &a.Kind, &a.Title, &a.Reason, &a.Status, &a.CreatedAt, &a.DecidedAt)
	return a, err
}

// plan reads the record and proposes follow-ups for results outside their range. It only proposes; the
// user decides. Re-running never re-proposes something already proposed, approved or rejected.
func (m *Module) plan(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	if !m.granted(ctx, profileID, "propose_actions") {
		httpx.WriteError(w, 403, "PERMISSION_REQUIRED", "Allow the agent to suggest follow-ups first.", nil)
		return
	}
	if !m.granted(ctx, profileID, "read_labs") {
		httpx.WriteError(w, 403, "PERMISSION_REQUIRED", "Allow the agent to read your lab results first.", nil)
		return
	}
	labs, err := m.flaggedLabs(ctx, owner, profileID)
	if err != nil {
		httpx.WriteError(w, 500, "PLAN_FAILED", "The agent could not read your records.", nil)
		return
	}
	created := 0
	for _, l := range labs {
		tag, err := m.DB.Pool.Exec(ctx, `INSERT INTO agent_actions(profile_id,owner_id,kind,title,reason,dedupe_key) VALUES($1,$2,'follow_up',$3,$4,$5) ON CONFLICT(profile_id,dedupe_key) DO NOTHING`,
			profileID, owner, "Discuss "+l.Name+" with your clinician", l.describe(), "lab:"+l.Code+":"+l.ObservationID)
		if err != nil {
			httpx.WriteError(w, 500, "PLAN_FAILED", "The agent could not save its suggestions.", nil)
			return
		}
		created += int(tag.RowsAffected())
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"steps": []map[string]any{
			{"label": "Read your lab results", "detail": len(labs)},
			{"label": "Compared each with its reference range", "detail": len(labs)},
			{"label": "Suggested follow-ups for you to approve", "detail": created},
		},
		"labs_outside_range": len(labs),
		"proposed":           created,
	})
}

func (m *Module) listActions(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	status := r.URL.Query().Get("status")
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+actionColumns+` FROM agent_actions WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR status=$3) ORDER BY created_at DESC LIMIT 100`,
		chi.URLParam(r, "profileID"), httpx.Owner(r), status)
	if err != nil {
		httpx.WriteError(w, 500, "ACTIONS_UNAVAILABLE", "Suggestions could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []action{}
	for rows.Next() {
		if a, err := scanAction(rows); err == nil {
			out = append(out, a)
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"actions": out})
}

func (m *Module) approve(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID, id := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "actionID")
	tx, err := m.DB.Pool.Begin(ctx)
	if err != nil {
		httpx.WriteError(w, 500, "APPROVE_FAILED", "The suggestion could not be approved.", nil)
		return
	}
	defer tx.Rollback(ctx)
	var title, reason string
	err = tx.QueryRow(ctx, `SELECT title,reason FROM agent_actions WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status='proposed' FOR UPDATE`, id, profileID, owner).Scan(&title, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, 404, "ACTION_NOT_FOUND", "That suggestion is not waiting for a decision.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "APPROVE_FAILED", "The suggestion could not be approved.", nil)
		return
	}
	var followUp string
	if err = tx.QueryRow(ctx, `INSERT INTO follow_ups(owner_id,profile_id,kind,title,notes) VALUES($1,$2,'other',$3,$4) RETURNING id::text`, owner, profileID, title, reason).Scan(&followUp); err != nil {
		httpx.WriteError(w, 500, "APPROVE_FAILED", "The suggestion could not be approved.", nil)
		return
	}
	a, err := scanAction(tx.QueryRow(ctx, `UPDATE agent_actions SET status='approved',result_id=$2,decided_at=now() WHERE id=$1 RETURNING `+actionColumns, id, followUp))
	if err != nil || tx.Commit(ctx) != nil {
		httpx.WriteError(w, 500, "APPROVE_FAILED", "The suggestion could not be approved.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"action": a, "follow_up_id": followUp})
}

func (m *Module) reject(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	a, err := scanAction(m.DB.Pool.QueryRow(r.Context(), `UPDATE agent_actions SET status='rejected',decided_at=now() WHERE id=$1 AND profile_id=$2 AND owner_id=$3 AND status='proposed' RETURNING `+actionColumns,
		chi.URLParam(r, "actionID"), chi.URLParam(r, "profileID"), httpx.Owner(r)))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, 404, "ACTION_NOT_FOUND", "That suggestion is not waiting for a decision.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "REJECT_FAILED", "The suggestion could not be dismissed.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"action": a})
}

// activity is the agent's log: what it suggested and what the user decided, newest first.
func (m *Module) activity(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `
		SELECT 'suggested' AS event, title, created_at AS at FROM agent_actions WHERE profile_id=$1 AND owner_id=$2
		UNION ALL SELECT status, title, decided_at FROM agent_actions WHERE profile_id=$1 AND owner_id=$2 AND decided_at IS NOT NULL
		ORDER BY at DESC LIMIT 100`, chi.URLParam(r, "profileID"), httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "ACTIVITY_UNAVAILABLE", "The activity log could not be loaded.", nil)
		return
	}
	defer rows.Close()
	type event struct {
		Event string    `json:"event"`
		Title string    `json:"title"`
		At    time.Time `json:"at"`
	}
	out := []event{}
	for rows.Next() {
		var e event
		if rows.Scan(&e.Event, &e.Title, &e.At) == nil {
			out = append(out, e)
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"events": out})
}
