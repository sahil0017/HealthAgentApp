package governance

import (
	"errors"
	"net/http"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const maxRetentionDays = 3650

type retentionPolicy struct {
	AIHistoryDays      *int       `json:"ai_history_days"`
	DeviceReadingsDays *int       `json:"device_readings_days"`
	LastAppliedAt      *time.Time `json:"last_applied_at"`
}

func (m *Module) loadRetention(r *http.Request, profileID string) (retentionPolicy, error) {
	var p retentionPolicy
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT ai_history_days,device_readings_days,last_applied_at FROM retention_policies WHERE profile_id=$1`, profileID).
		Scan(&p.AIHistoryDays, &p.DeviceReadingsDays, &p.LastAppliedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return retentionPolicy{}, nil
	}
	return p, err
}

func (m *Module) getRetention(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	p, err := m.loadRetention(r, chi.URLParam(r, "profileID"))
	if err != nil {
		httpx.WriteError(w, 500, "RETENTION_READ_FAILED", "The retention policy could not be read.", nil)
		return
	}
	httpx.WriteJSON(w, 200, p)
}

// setRetention replaces the policy. A missing or null field means keep that data indefinitely.
func (m *Module) setRetention(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	var in retentionPolicy
	if httpx.DecodeJSON(r, 1<<10, &in) != nil || !validDays(in.AIHistoryDays) || !validDays(in.DeviceReadingsDays) {
		httpx.WriteError(w, 400, "INVALID_RETENTION", "Retention must be between 1 and 3650 days, or empty to keep data.", nil)
		return
	}
	_, err := m.DB.Pool.Exec(r.Context(), `INSERT INTO retention_policies(profile_id,ai_history_days,device_readings_days) VALUES($1,$2,$3)
		ON CONFLICT(profile_id) DO UPDATE SET ai_history_days=EXCLUDED.ai_history_days,device_readings_days=EXCLUDED.device_readings_days,updated_at=now()`,
		profileID, in.AIHistoryDays, in.DeviceReadingsDays)
	if err == nil {
		_, err = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'retention.updated','profile',$2::uuid::text,jsonb_build_object('ai_history_days',$3::int,'device_readings_days',$4::int))`,
			httpx.Owner(r), profileID, in.AIHistoryDays, in.DeviceReadingsDays)
	}
	if err != nil {
		httpx.WriteError(w, 500, "RETENTION_WRITE_FAILED", "The retention policy could not be saved.", nil)
		return
	}
	p, _ := m.loadRetention(r, profileID)
	httpx.WriteJSON(w, 200, p)
}

// applyRetention purges expired data now and reports only counts.
func (m *Module) applyRetention(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	var ai, dev int64
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT ai_history_removed,device_readings_removed FROM cortex_apply_retention($1::uuid)`, profileID).Scan(&ai, &dev); err != nil {
		httpx.WriteError(w, 500, "RETENTION_APPLY_FAILED", "Retention could not be applied.", nil)
		return
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'retention.applied','profile',$2::uuid::text,jsonb_build_object('ai_history_removed',$3::bigint,'device_readings_removed',$4::bigint))`, httpx.Owner(r), profileID, ai, dev)
	httpx.WriteJSON(w, 200, map[string]int64{"ai_history_removed": ai, "device_readings_removed": dev})
}

func validDays(d *int) bool { return d == nil || (*d >= 1 && *d <= maxRetentionDays) }
