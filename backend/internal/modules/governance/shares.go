package governance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxActiveShares = 10

var shareScopes = []string{"summary", "labs", "history", "follow_ups"}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

type shareRecord struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

const shareStatusSQL = `CASE WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=now() THEN 'expired' WHEN accepted_at IS NULL THEN 'pending' ELSE 'active' END`

func (m *Module) auditShare(r *http.Request, ownerID, profileID, action, shareID string) {
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,$3,'profile_share',$4)`, ownerID, profileID, action, shareID)
}

func (m *Module) createShare(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	var in struct {
		Label         string   `json:"label"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expires_in_days"`
	}
	if httpx.DecodeJSON(r, 4<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_SHARE", "The share could not be read.", nil)
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.ExpiresInDays == 0 {
		in.ExpiresInDays = 30
	}
	valid := in.Label != "" && len(in.Label) <= 80 && in.ExpiresInDays >= 1 && in.ExpiresInDays <= 365 && len(in.Scopes) > 0
	for _, s := range in.Scopes {
		valid = valid && slices.Contains(shareScopes, s)
	}
	if !valid {
		httpx.WriteError(w, 400, "INVALID_SHARE", "A label, at least one supported scope and 1-365 days are required.", map[string]string{"scopes": strings.Join(shareScopes, ",")})
		return
	}
	var active int
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM profile_shares WHERE profile_id=$1 AND revoked_at IS NULL AND expires_at>now()`, profileID).Scan(&active)
	if active >= maxActiveShares {
		httpx.WriteError(w, 409, "SHARE_LIMIT", "Revoke an existing share before creating another.", nil)
		return
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		httpx.WriteError(w, 500, "SHARE_CREATE_FAILED", "The share could not be created.", nil)
		return
	}
	code := hex.EncodeToString(raw)
	scopes := slices.Compact(slices.Sorted(slices.Values(in.Scopes)))
	var rec shareRecord
	err := m.DB.Pool.QueryRow(r.Context(), `INSERT INTO profile_shares(profile_id,owner_id,label,scopes,code_hash,expires_at) VALUES($1,$2,$3,$4,$5,now()+make_interval(days=>$6))
		RETURNING id::text,label,scopes,`+shareStatusSQL+`,expires_at,accepted_at,created_at`, profileID, httpx.Owner(r), in.Label, scopes, hashCode(code), in.ExpiresInDays).
		Scan(&rec.ID, &rec.Label, &rec.Scopes, &rec.Status, &rec.ExpiresAt, &rec.AcceptedAt, &rec.CreatedAt)
	if err != nil {
		httpx.WriteError(w, 500, "SHARE_CREATE_FAILED", "The share could not be created.", nil)
		return
	}
	m.auditShare(r, httpx.Owner(r), profileID, "share.created", rec.ID)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"share": rec, "code": code})
}

func (m *Module) listShares(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text,label,scopes,`+shareStatusSQL+`,expires_at,accepted_at,created_at FROM profile_shares WHERE profile_id=$1 ORDER BY created_at DESC`, chi.URLParam(r, "profileID"))
	if err != nil {
		httpx.WriteError(w, 500, "SHARE_LIST_FAILED", "Shares could not be listed.", nil)
		return
	}
	defer rows.Close()
	out := []shareRecord{}
	for rows.Next() {
		var s shareRecord
		if err := rows.Scan(&s.ID, &s.Label, &s.Scopes, &s.Status, &s.ExpiresAt, &s.AcceptedAt, &s.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "SHARE_LIST_FAILED", "Shares could not be listed.", nil)
			return
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, 200, map[string]any{"shares": out})
}

func (m *Module) revokeShare(w http.ResponseWriter, r *http.Request) {
	profileID, id := chi.URLParam(r, "profileID"), chi.URLParam(r, "shareID")
	if _, err := uuid.Parse(id); err != nil || !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "SHARE_NOT_FOUND", "The requested share could not be found.", nil)
		return
	}
	tag, err := m.DB.Pool.Exec(r.Context(), `UPDATE profile_shares SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, id, profileID, httpx.Owner(r))
	if err != nil || tag.RowsAffected() == 0 {
		httpx.WriteError(w, 404, "SHARE_NOT_FOUND", "The requested share could not be found.", nil)
		return
	}
	m.auditShare(r, httpx.Owner(r), profileID, "share.revoked", id)
	w.WriteHeader(http.StatusNoContent)
}

// acceptShare redeems an invite code once; the redeemer becomes the read-only grantee.
func (m *Module) acceptShare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if httpx.DecodeJSON(r, 1<<10, &in) != nil || strings.TrimSpace(in.Code) == "" {
		httpx.WriteError(w, 400, "INVALID_SHARE_CODE", "A share code is required.", nil)
		return
	}
	var id, profileID, ownerID, label string
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT id::text,profile_id::text,owner_id,label FROM profile_shares WHERE code_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now()`, hashCode(in.Code)).Scan(&id, &profileID, &ownerID, &label)
	if err != nil {
		httpx.WriteError(w, 404, "INVALID_SHARE_CODE", "This share code is invalid, already used, revoked or expired.", nil)
		return
	}
	if ownerID == httpx.Owner(r) {
		httpx.WriteError(w, 400, "OWN_SHARE", "You cannot accept a share of your own profile.", nil)
		return
	}
	tag, err := m.DB.Pool.Exec(r.Context(), `UPDATE profile_shares SET grantee_id=$2,accepted_at=now() WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, id, httpx.Owner(r))
	if err != nil || tag.RowsAffected() == 0 {
		httpx.WriteError(w, 404, "INVALID_SHARE_CODE", "This share code is invalid, already used, revoked or expired.", nil)
		return
	}
	m.auditShare(r, ownerID, profileID, "share.accepted", id)
	httpx.WriteJSON(w, 200, map[string]string{"id": id, "label": label})
}

func (m *Module) sharedWithMe(w http.ResponseWriter, r *http.Request) {
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT s.id::text,p.display_name,s.scopes,s.expires_at FROM profile_shares s JOIN profiles p ON p.id=s.profile_id
		WHERE s.grantee_id=$1 AND s.revoked_at IS NULL AND s.expires_at>now() ORDER BY s.accepted_at DESC`, httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "SHARE_LIST_FAILED", "Shares could not be listed.", nil)
		return
	}
	defer rows.Close()
	type item struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"display_name"`
		Scopes      []string  `json:"scopes"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	out := []item{}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.ID, &i.DisplayName, &i.Scopes, &i.ExpiresAt); err != nil {
			httpx.WriteError(w, 500, "SHARE_LIST_FAILED", "Shares could not be listed.", nil)
			return
		}
		out = append(out, i)
	}
	httpx.WriteJSON(w, 200, map[string]any{"shares": out})
}

// viewShared returns the read-only slice of a profile the owner chose to share. Each view is audited
// on the owner's access history with the share id only.
func (m *Module) viewShared(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "shareID")
	var profileID, ownerID, name string
	var scopes []string
	var err error
	if _, perr := uuid.Parse(id); perr != nil {
		err = perr
	} else {
		err = m.DB.Pool.QueryRow(r.Context(), `SELECT s.profile_id::text,s.owner_id,s.scopes,p.display_name FROM profile_shares s JOIN profiles p ON p.id=s.profile_id
			WHERE s.id=$1 AND s.grantee_id=$2 AND s.revoked_at IS NULL AND s.expires_at>now()`, id, httpx.Owner(r)).Scan(&profileID, &ownerID, &scopes, &name)
	}
	if err != nil {
		httpx.WriteError(w, 404, "SHARE_NOT_FOUND", "The requested share could not be found.", nil)
		return
	}
	out := map[string]any{"display_name": name, "scopes": scopes, "read_only": true}
	load := func(key, sql string) bool {
		var raw []byte
		if m.DB.Pool.QueryRow(r.Context(), sql, profileID).Scan(&raw) != nil {
			httpx.WriteError(w, 500, "SHARE_READ_FAILED", "The shared data could not be read.", nil)
			return false
		}
		out[key] = json.RawMessage(raw)
		return true
	}
	queries := map[string]string{
		"summary":    `SELECT jsonb_build_object('date_of_birth',date_of_birth,'sex',sex,'blood_group',blood_group,'height_cm',height_cm) FROM profiles WHERE id=$1`,
		"labs":       `SELECT COALESCE(jsonb_agg(x ORDER BY x.name),'[]') FROM (SELECT DISTINCT ON (name) name,value,unit,observed_at,reference_range FROM observations WHERE profile_id=$1 AND verification_status='VERIFIED' ORDER BY name,observed_at DESC NULLS LAST,created_at DESC) x`,
		"history":    `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'title',title,'started_on',started_on) ORDER BY created_at),'[]') FROM history_entries WHERE profile_id=$1 AND source='user' AND status='active'`,
		"follow_ups": `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'title',title,'due_on',due_on) ORDER BY due_on NULLS LAST),'[]') FROM follow_ups WHERE profile_id=$1 AND status='open'`,
	}
	for _, scope := range scopes {
		if !load(scope, queries[scope]) {
			return
		}
	}
	m.auditShare(r, ownerID, profileID, "share.viewed", id)
	httpx.WriteJSON(w, 200, out)
}

func (m *Module) leaveShare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "shareID")
	if _, err := uuid.Parse(id); err != nil {
		httpx.WriteError(w, 404, "SHARE_NOT_FOUND", "The requested share could not be found.", nil)
		return
	}
	var profileID, ownerID string
	err := m.DB.Pool.QueryRow(r.Context(), `UPDATE profile_shares SET revoked_at=now() WHERE id=$1 AND grantee_id=$2 AND revoked_at IS NULL RETURNING profile_id::text,owner_id`, id, httpx.Owner(r)).Scan(&profileID, &ownerID)
	if err != nil {
		httpx.WriteError(w, 404, "SHARE_NOT_FOUND", "The requested share could not be found.", nil)
		return
	}
	m.auditShare(r, ownerID, profileID, "share.left", id)
	w.WriteHeader(http.StatusNoContent)
}
