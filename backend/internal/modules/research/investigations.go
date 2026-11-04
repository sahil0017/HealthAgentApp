package research

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxInvestigationsPerProfile = 100
	maxItemsPerInvestigation    = 100
	refreshSourceLimit          = 5
)

type investigation struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Question        string     `json:"question"`
	Status          string     `json:"status"`
	SourceCount     int        `json:"source_count"`
	NoteCount       int        `json:"note_count"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastRefreshedAt *time.Time `json:"last_refreshed_at"`
}

type investigationItem struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	SourceID   *string   `json:"source_id,omitempty"`
	Identifier *string   `json:"identifier,omitempty"`
	Title      *string   `json:"title,omitempty"`
	URL        *string   `json:"url,omitempty"`
	Excerpt    *string   `json:"excerpt,omitempty"`
	Note       *string   `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

const investigationColumns = `i.id::text,i.title,i.question,i.status,
	(SELECT count(*) FROM research_investigation_items t WHERE t.investigation_id=i.id AND t.kind='source'),
	(SELECT count(*) FROM research_investigation_items t WHERE t.investigation_id=i.id AND t.kind='note'),
	i.created_at,i.updated_at,i.last_refreshed_at`

func scanInvestigation(row interface{ Scan(...any) error }) (investigation, error) {
	var v investigation
	err := row.Scan(&v.ID, &v.Title, &v.Question, &v.Status, &v.SourceCount, &v.NoteCount, &v.CreatedAt, &v.UpdatedAt, &v.LastRefreshedAt)
	return v, err
}

func (m *Module) profileOr404(w http.ResponseWriter, r *http.Request) bool {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return false
	}
	return true
}

// ownedInvestigation resolves {investigationID} within the caller's profile.
func (m *Module) ownedInvestigation(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "investigationID")
	var found bool
	if _, err := uuid.Parse(id); err == nil {
		_ = m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM research_investigations WHERE id=$1 AND profile_id=$2 AND owner_id=$3)`, id, chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&found)
	}
	if !found {
		httpx.WriteError(w, 404, "INVESTIGATION_NOT_FOUND", "The requested investigation could not be found.", nil)
	}
	return id, found
}

func (m *Module) audit(r *http.Request, action, id string) {
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,$3,'research_investigation',$4)`, httpx.Owner(r), chi.URLParam(r, "profileID"), action, id)
}

func (m *Module) createInvestigation(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	profileID := chi.URLParam(r, "profileID")
	var in struct{ Title, Question string }
	if httpx.DecodeJSON(r, 8<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_INVESTIGATION", "The investigation could not be read.", nil)
		return
	}
	in.Question, in.Title = strings.TrimSpace(in.Question), strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = truncate(in.Question, 80)
	}
	if len(in.Question) < 2 || len(in.Question) > 500 || len(in.Title) > 200 {
		httpx.WriteError(w, 400, "INVALID_INVESTIGATION", "A question of 2-500 characters is required.", nil)
		return
	}
	var count int
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM research_investigations WHERE profile_id=$1`, profileID).Scan(&count)
	if count >= maxInvestigationsPerProfile {
		httpx.WriteError(w, 409, "INVESTIGATION_LIMIT", "Delete an investigation before saving another.", nil)
		return
	}
	v, err := scanInvestigation(m.DB.Pool.QueryRow(r.Context(), `WITH i AS (INSERT INTO research_investigations(profile_id,owner_id,title,question) VALUES($1,$2,$3,$4) RETURNING *) SELECT `+investigationColumns+` FROM i`, profileID, httpx.Owner(r), in.Title, in.Question))
	if err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_CREATE_FAILED", "The investigation could not be saved.", nil)
		return
	}
	m.audit(r, "research.investigation_created", v.ID)
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func (m *Module) listInvestigations(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "open" && status != "archived" {
		httpx.WriteError(w, 400, "INVALID_FILTER", "status must be open or archived.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+investigationColumns+` FROM research_investigations i WHERE i.profile_id=$1 AND i.owner_id=$2 AND ($3='' OR i.status=$3) ORDER BY i.updated_at DESC`, chi.URLParam(r, "profileID"), httpx.Owner(r), status)
	if err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_LIST_FAILED", "Investigations could not be listed.", nil)
		return
	}
	defer rows.Close()
	out := []investigation{}
	for rows.Next() {
		v, err := scanInvestigation(rows)
		if err != nil {
			httpx.WriteError(w, 500, "INVESTIGATION_LIST_FAILED", "Investigations could not be listed.", nil)
			return
		}
		out = append(out, v)
	}
	httpx.WriteJSON(w, 200, map[string]any{"investigations": out})
}

func (m *Module) getInvestigation(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	v, err := scanInvestigation(m.DB.Pool.QueryRow(r.Context(), `SELECT `+investigationColumns+` FROM research_investigations i WHERE i.id=$1`, id))
	if err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_READ_FAILED", "The investigation could not be read.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT t.id::text,t.kind,t.source_id::text,s.identifier,s.title,s.url,t.excerpt,t.note,t.created_at
		FROM research_investigation_items t LEFT JOIN research_sources s ON s.id=t.source_id WHERE t.investigation_id=$1 ORDER BY t.created_at,t.id`, id)
	if err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_READ_FAILED", "The investigation could not be read.", nil)
		return
	}
	defer rows.Close()
	items := []investigationItem{}
	for rows.Next() {
		var it investigationItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.SourceID, &it.Identifier, &it.Title, &it.URL, &it.Excerpt, &it.Note, &it.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "INVESTIGATION_READ_FAILED", "The investigation could not be read.", nil)
			return
		}
		items = append(items, it)
	}
	httpx.WriteJSON(w, 200, map[string]any{"investigation": v, "items": items, "disclaimer": "Research summaries are general information from approved public sources, not medical advice."})
}

func (m *Module) updateInvestigation(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	var in struct {
		Title    *string `json:"title"`
		Question *string `json:"question"`
		Status   *string `json:"status"`
	}
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || (in.Title == nil && in.Question == nil && in.Status == nil) {
		httpx.WriteError(w, 400, "INVALID_INVESTIGATION", "Send a title, question or status to change.", nil)
		return
	}
	for _, p := range []*string{in.Title, in.Question} {
		if p != nil {
			*p = strings.TrimSpace(*p)
		}
	}
	bad := (in.Title != nil && (*in.Title == "" || len(*in.Title) > 200)) ||
		(in.Question != nil && (len(*in.Question) < 2 || len(*in.Question) > 500)) ||
		(in.Status != nil && *in.Status != "open" && *in.Status != "archived")
	if bad {
		httpx.WriteError(w, 400, "INVALID_INVESTIGATION", "The title, question or status is not valid.", nil)
		return
	}
	v, err := scanInvestigation(m.DB.Pool.QueryRow(r.Context(), `WITH i AS (UPDATE research_investigations SET title=COALESCE($2,title),question=COALESCE($3,question),status=COALESCE($4,status),updated_at=now() WHERE id=$1 RETURNING *) SELECT `+investigationColumns+` FROM i`, id, in.Title, in.Question, in.Status))
	if err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_UPDATE_FAILED", "The investigation could not be updated.", nil)
		return
	}
	m.audit(r, "research.investigation_updated", id)
	httpx.WriteJSON(w, 200, v)
}

func (m *Module) deleteInvestigation(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	if _, err := m.DB.Pool.Exec(r.Context(), `DELETE FROM research_investigations WHERE id=$1`, id); err != nil {
		httpx.WriteError(w, 500, "INVESTIGATION_DELETE_FAILED", "The investigation could not be deleted.", nil)
		return
	}
	m.audit(r, "research.investigation_deleted", id)
	w.WriteHeader(http.StatusNoContent)
}

// addInvestigationItem attaches an approved research source or a personal note.
func (m *Module) addInvestigationItem(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	var in struct {
		SourceID string `json:"source_id"`
		Note     string `json:"note"`
	}
	if httpx.DecodeJSON(r, 8<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_ITEM", "The item could not be read.", nil)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if (in.SourceID == "") == (in.Note == "") || len(in.Note) > 2000 {
		httpx.WriteError(w, 400, "INVALID_ITEM", "Send either an approved source_id or a note of up to 2000 characters.", nil)
		return
	}
	var count int
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM research_investigation_items WHERE investigation_id=$1`, id).Scan(&count)
	if count >= maxItemsPerInvestigation {
		httpx.WriteError(w, 409, "INVESTIGATION_FULL", "This investigation has reached its item limit.", nil)
		return
	}
	var itemID string
	var err error
	if in.Note != "" {
		err = m.DB.Pool.QueryRow(r.Context(), `INSERT INTO research_investigation_items(investigation_id,kind,note) VALUES($1,'note',$2) RETURNING id::text`, id, in.Note).Scan(&itemID)
	} else {
		if _, perr := uuid.Parse(in.SourceID); perr != nil {
			httpx.WriteError(w, 404, "SOURCE_NOT_FOUND", "That research source is not approved or does not exist.", nil)
			return
		}
		err = m.DB.Pool.QueryRow(r.Context(), `INSERT INTO research_investigation_items(investigation_id,kind,source_id,excerpt)
			SELECT $1,'source',s.id,(SELECT left(c.content,2000) FROM research_chunks c WHERE c.source_id=s.id ORDER BY c.ordinal LIMIT 1) FROM research_sources s WHERE s.id=$2
			ON CONFLICT DO NOTHING RETURNING id::text`, id, in.SourceID).Scan(&itemID)
		if err != nil {
			var exists bool
			_ = m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM research_sources WHERE id=$1)`, in.SourceID).Scan(&exists)
			if !exists {
				httpx.WriteError(w, 404, "SOURCE_NOT_FOUND", "That research source is not approved or does not exist.", nil)
			} else {
				httpx.WriteError(w, 409, "SOURCE_ALREADY_ADDED", "That source is already in this investigation.", nil)
			}
			return
		}
	}
	if err != nil {
		httpx.WriteError(w, 500, "ITEM_CREATE_FAILED", "The item could not be saved.", nil)
		return
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE research_investigations SET updated_at=now() WHERE id=$1`, id)
	m.audit(r, "research.investigation_item_added", id)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": itemID})
}

func (m *Module) deleteInvestigationItem(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	itemID := chi.URLParam(r, "itemID")
	if _, err := uuid.Parse(itemID); err != nil {
		httpx.WriteError(w, 404, "ITEM_NOT_FOUND", "The requested item could not be found.", nil)
		return
	}
	tag, err := m.DB.Pool.Exec(r.Context(), `DELETE FROM research_investigation_items WHERE id=$1 AND investigation_id=$2`, itemID, id)
	if err != nil || tag.RowsAffected() == 0 {
		httpx.WriteError(w, 404, "ITEM_NOT_FOUND", "The requested item could not be found.", nil)
		return
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE research_investigations SET updated_at=now() WHERE id=$1`, id)
	w.WriteHeader(http.StatusNoContent)
}

// refreshInvestigation looks for approved sources matching the question and attaches new ones. It reads
// only human-approved research, never the open web and never the profile's own data.
func (m *Module) refreshInvestigation(w http.ResponseWriter, r *http.Request) {
	if !m.profileOr404(w, r) {
		return
	}
	id, ok := m.ownedInvestigation(w, r)
	if !ok {
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `
WITH q AS (SELECT replace(plainto_tsquery('english', question)::text, '&', '|')::tsquery AS q FROM research_investigations WHERE id=$1),
hits AS (
  SELECT c.source_id, max(ts_rank_cd(c.content_tsv, q.q)) AS rank, (array_agg(left(c.content,2000) ORDER BY ts_rank_cd(c.content_tsv, q.q) DESC))[1] AS excerpt
  FROM research_chunks c, q WHERE c.content_tsv @@ q.q GROUP BY c.source_id ORDER BY rank DESC LIMIT $2
)
INSERT INTO research_investigation_items(investigation_id,kind,source_id,excerpt)
SELECT $1,'source',h.source_id,h.excerpt FROM hits h
WHERE (SELECT count(*) FROM research_investigation_items WHERE investigation_id=$1) < $3
ON CONFLICT DO NOTHING RETURNING id::text`, id, refreshSourceLimit, maxItemsPerInvestigation)
	if err != nil {
		httpx.WriteError(w, 500, "REFRESH_FAILED", "The investigation could not be refreshed.", nil)
		return
	}
	added := 0
	for rows.Next() {
		added++
	}
	rows.Close()
	_, _ = m.DB.Pool.Exec(r.Context(), `UPDATE research_investigations SET last_refreshed_at=now(),updated_at=now() WHERE id=$1`, id)
	m.audit(r, "research.investigation_refreshed", id)
	httpx.WriteJSON(w, 200, map[string]int{"added": added})
}
