package research

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"uuid"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var errNotPending = errors.New("entry is not pending")

// isAdmin gates knowledge review. Production uses an explicit allowlist; development sessions are the local operator.
func (m *Module) isAdmin(r *http.Request) bool {
	id := httpx.Owner(r)
	for _, v := range strings.Split(os.Getenv("CORTEX_ADMIN_OWNER_IDS"), ",") {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) == id {
			return true
		}
	}
	return strings.TrimSpace(os.Getenv("CORTEX_AUTH_MODE")) == "development"
}

func (m *Module) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !m.isAdmin(r) {
		httpx.WriteError(w, http.StatusForbidden, "ADMIN_REQUIRED", "Reviewing knowledge requires an administrator.", nil)
		return false
	}
	return true
}

type knowledgeEntry struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Key        string          `json:"key"`
	Payload    json.RawMessage `json:"payload"`
	Evidence   json.RawMessage `json:"evidence"`
	Agreement  int             `json:"agreement"`
	Status     string          `json:"status"`
	ReviewedBy *string         `json:"reviewed_by,omitempty"`
	ReviewNote *string         `json:"review_note,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

func (m *Module) listKnowledge(w http.ResponseWriter, r *http.Request) {
	if !m.requireAdmin(w, r) {
		return
	}
	status, kind := r.URL.Query().Get("status"), r.URL.Query().Get("kind")
	if status == "" {
		status = "pending"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text,kind,entry_key,payload,evidence,agreement,status,reviewed_by,review_note,created_at::text
		FROM knowledge_entries WHERE status=$1::text AND ($2::text='' OR kind=$2::text) ORDER BY agreement DESC, created_at LIMIT $3::int OFFSET $4::int`, status, kind, limit, max(offset, 0))
	if err != nil {
		httpx.WriteError(w, 500, "KNOWLEDGE_UNAVAILABLE", "Knowledge entries could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []knowledgeEntry{}
	for rows.Next() {
		var e knowledgeEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Key, &e.Payload, &e.Evidence, &e.Agreement, &e.Status, &e.ReviewedBy, &e.ReviewNote, &e.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "KNOWLEDGE_UNAVAILABLE", "Knowledge entries could not be loaded.", nil)
			return
		}
		items = append(items, e)
	}
	var queued, failed int
	_ = m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE status IN ('queued','running')), count(*) FILTER (WHERE status='failed') FROM research_tasks`).Scan(&queued, &failed)
	httpx.WriteJSON(w, 200, map[string]any{"entries": items, "research_queue": map[string]int{"pending": queued, "failed": failed}})
}

func (m *Module) requestResearch(w http.ResponseWriter, r *http.Request) {
	if !m.requireAdmin(w, r) {
		return
	}
	var in struct {
		Kind string `json:"kind"`
		Term string `json:"term"`
	}
	if httpx.DecodeJSON(r, 4<<10, &in) != nil {
		httpx.WriteError(w, 400, "INVALID_RESEARCH_REQUEST", "Kind and term are required.", nil)
		return
	}
	in.Term = strings.TrimSpace(in.Term)
	if (in.Kind != "biomarker" && in.Kind != "literature") || len(in.Term) < 2 || len(in.Term) > 120 {
		httpx.WriteError(w, 400, "INVALID_RESEARCH_REQUEST", "Kind must be biomarker or literature and term 2-120 characters.", nil)
		return
	}
	if err := m.Knowledge.Enqueue(r.Context(), in.Kind, in.Term); err != nil {
		httpx.WriteError(w, 500, "RESEARCH_ENQUEUE_FAILED", "The research request could not be queued.", nil)
		return
	}
	m.auditKnowledge(r.Context(), httpx.Owner(r), "knowledge.research_requested", map[string]string{"kind": in.Kind, "term": in.Term})
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (m *Module) approveKnowledge(w http.ResponseWriter, r *http.Request) {
	m.reviewKnowledge(w, r, true)
}

func (m *Module) rejectKnowledge(w http.ResponseWriter, r *http.Request) {
	m.reviewKnowledge(w, r, false)
}

func (m *Module) reviewKnowledge(w http.ResponseWriter, r *http.Request, approve bool) {
	if !m.requireAdmin(w, r) {
		return
	}
	id := chi.URLParam(r, "entryID")
	if _, err := uuid.Parse(id); err != nil {
		httpx.WriteError(w, 400, "INVALID_ENTRY", "The entry ID is invalid.", nil)
		return
	}
	var in struct {
		Note     string `json:"note"`
		Category string `json:"category"`
	}
	_ = httpx.DecodeJSON(r, 4<<10, &in)
	var err error
	if approve && in.Category != "" {
		// The reviewer decides the panel a learned biomarker belongs to; LOINC cannot say reliably.
		if !biomarker.ValidCategory(in.Category) {
			httpx.Invalid(w, "INVALID_CATEGORY", "Category is not a supported panel.", "category")
			return
		}
		if _, err := m.DB.Pool.Exec(r.Context(), `UPDATE knowledge_entries SET payload=payload||jsonb_build_object('category',$2::text) WHERE id=$1::uuid AND kind='biomarker' AND status='pending'`, id, in.Category); err != nil {
			httpx.WriteError(w, 500, "REVIEW_FAILED", "The entry could not be reviewed.", nil)
			return
		}
	}
	if approve {
		err = m.approveEntry(r.Context(), id, httpx.Owner(r), in.Note)
	} else {
		err = m.rejectEntry(r.Context(), id, httpx.Owner(r), in.Note)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.WriteError(w, 404, "ENTRY_NOT_FOUND", "The knowledge entry could not be found.", nil)
	case errors.Is(err, errNotPending):
		httpx.WriteError(w, 409, "ENTRY_NOT_PENDING", "This entry has already been reviewed.", nil)
	case err != nil:
		httpx.WriteError(w, 502, "REVIEW_FAILED", "The decision could not be applied: "+err.Error(), nil)
	default:
		httpx.WriteJSON(w, 200, map[string]string{"id": id, "status": map[bool]string{true: "approved", false: "rejected"}[approve]})
	}
}

// bulkApprove approves pending biomarker and unit entries only; literature stays an individual decision.
func (m *Module) bulkApproveKnowledge(w http.ResponseWriter, r *http.Request) {
	if !m.requireAdmin(w, r) {
		return
	}
	var in struct {
		Kind         string `json:"kind"`
		MinAgreement int    `json:"min_agreement"`
		Limit        int    `json:"limit"`
	}
	if httpx.DecodeJSON(r, 4<<10, &in) != nil || (in.Kind != "biomarker" && in.Kind != "unit") {
		httpx.WriteError(w, 400, "INVALID_BULK_REQUEST", "Kind must be biomarker or unit.", nil)
		return
	}
	if in.Limit <= 0 || in.Limit > 500 {
		in.Limit = 500
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text FROM knowledge_entries WHERE status='pending' AND kind=$1::text AND agreement>=$2::int ORDER BY created_at LIMIT $3::int`, in.Kind, in.MinAgreement, in.Limit)
	if err != nil {
		httpx.WriteError(w, 500, "KNOWLEDGE_UNAVAILABLE", "Knowledge entries could not be loaded.", nil)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	approved, failed := 0, 0
	for _, id := range ids {
		if m.approveEntry(r.Context(), id, httpx.Owner(r), "bulk approval") == nil {
			approved++
		} else {
			failed++
		}
	}
	httpx.WriteJSON(w, 200, map[string]int{"approved": approved, "failed": failed})
}

func (m *Module) rejectEntry(ctx context.Context, id, reviewer, note string) error {
	tag, err := m.DB.Pool.Exec(ctx, `UPDATE knowledge_entries SET status='rejected', reviewed_by=$2::text, review_note=NULLIF($3::text,''), reviewed_at=now() WHERE id=$1::uuid AND status='pending'`, id, reviewer, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return m.entryMissingOrReviewed(ctx, id)
	}
	m.auditKnowledge(ctx, reviewer, "knowledge.rejected", map[string]string{"entry_id": id})
	return nil
}

func (m *Module) entryMissingOrReviewed(ctx context.Context, id string) error {
	var status string
	if err := m.DB.Pool.QueryRow(ctx, `SELECT status FROM knowledge_entries WHERE id=$1::uuid`, id).Scan(&status); err != nil {
		return err
	}
	return errNotPending
}

type embeddedChunk struct {
	text   string
	vector []float32
}

func (m *Module) approveEntry(ctx context.Context, id, reviewer, note string) error {
	var kind, status string
	var payload json.RawMessage
	if err := m.DB.Pool.QueryRow(ctx, `SELECT kind,status,payload FROM knowledge_entries WHERE id=$1::uuid`, id).Scan(&kind, &status, &payload); err != nil {
		return err
	}
	if status != "pending" {
		return errNotPending
	}
	var chunks []embeddedChunk
	var model string
	if kind == "research_source" {
		var src researchPayload
		if err := json.Unmarshal(payload, &src); err != nil || src.Abstract == "" {
			return errors.New("invalid research source")
		}
		texts := chunkAbstract(src.Title, src.Abstract)
		m, vectors, err := embedding.Embed(ctx, texts, "passage")
		if err != nil {
			return err
		}
		model = m
		for i, t := range texts {
			chunks = append(chunks, embeddedChunk{t, vectors[i]})
		}
	}
	tx, err := m.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE knowledge_entries SET status='approved', reviewed_by=$2::text, review_note=NULLIF($3::text,''), reviewed_at=now() WHERE id=$1::uuid AND status='pending'`, id, reviewer, note)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotPending
	}
	if err := applyKnowledge(ctx, tx, kind, id, payload, reviewer, model, chunks); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	m.auditKnowledge(ctx, reviewer, "knowledge.approved", map[string]string{"entry_id": id, "kind": kind})
	return nil
}

type researchPayload struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Journal    string `json:"journal"`
	Year       *int   `json:"year"`
	URL        string `json:"url"`
	Abstract   string `json:"abstract"`
}

func applyKnowledge(ctx context.Context, tx pgx.Tx, kind, id string, payload json.RawMessage, reviewer, model string, chunks []embeddedChunk) error {
	switch kind {
	case "biomarker":
		var b struct {
			Code          string   `json:"code"`
			DisplayName   string   `json:"display_name"`
			Synonyms      []string `json:"synonyms"`
			SupportedUnit []string `json:"supported_units"`
			LoincNums     []string `json:"loinc_nums"`
			ReferenceNote *string  `json:"reference_note"`
			Category      *string  `json:"category"`
		}
		if err := json.Unmarshal(payload, &b); err != nil || b.Code == "" || b.DisplayName == "" || (b.Category != nil && !biomarker.ValidCategory(*b.Category)) {
			return errors.New("invalid biomarker entry")
		}
		_, err := tx.Exec(ctx, `INSERT INTO biomarker_catalog(code,display_name,synonyms,supported_units,loinc_nums,reference_note,status,source,approved_by,approved_at,category)
			VALUES($1::text,$2::text,$3::text[],$4::text[],$5::text[],to_jsonb($6::text),'approved','research-agent',$7::text,now(),$8::text)
			ON CONFLICT(code) DO UPDATE SET display_name=EXCLUDED.display_name,
				synonyms=(SELECT ARRAY(SELECT DISTINCT unnest(biomarker_catalog.synonyms || EXCLUDED.synonyms))),
				loinc_nums=EXCLUDED.loinc_nums, reference_note=EXCLUDED.reference_note, status='approved', source='research-agent', approved_by=EXCLUDED.approved_by, approved_at=now(), category=COALESCE(EXCLUDED.category, biomarker_catalog.category)`,
			strings.ToUpper(b.Code), b.DisplayName, nonNil(b.Synonyms), nonNil(b.SupportedUnit), nonNil(b.LoincNums), b.ReferenceNote, reviewer, b.Category)
		return err
	case "unit":
		var raw struct {
			Code string `json:"code"`
			Unit string `json:"unit"`
		}
		if err := json.Unmarshal(payload, &raw); err != nil || raw.Code == "" || raw.Unit == "" {
			return errors.New("invalid unit entry")
		}
		_, err := tx.Exec(ctx, `UPDATE biomarker_catalog SET learned_units=(SELECT ARRAY(SELECT DISTINCT unnest(learned_units || ARRAY[$2::text]))) WHERE code=$1::text`, raw.Code, raw.Unit)
		return err
	case "reference_range":
		var r struct {
			Code   string   `json:"code"`
			Unit   string   `json:"unit"`
			Sex    *string  `json:"sex"`
			AgeMin *int     `json:"age_min"`
			AgeMax *int     `json:"age_max"`
			Low    *float64 `json:"low"`
			High   *float64 `json:"high"`
			Source string   `json:"source"`
		}
		if err := json.Unmarshal(payload, &r); err != nil || r.Code == "" || r.Unit == "" || strings.TrimSpace(r.Source) == "" || (r.Low == nil && r.High == nil) || (r.Low != nil && r.High != nil && *r.Low > *r.High) || (r.Sex != nil && *r.Sex != "female" && *r.Sex != "male") {
			return errors.New("invalid reference range entry")
		}
		_, err := tx.Exec(ctx, `INSERT INTO reference_ranges(biomarker_code,unit,sex,age_min,age_max,low,high,source,knowledge_entry_id,approved_by)
			VALUES($1::text,$2::text,$3::text,$4::int,$5::int,$6::float8,$7::float8,$8::text,$9::uuid,$10::text)
			ON CONFLICT (biomarker_code, unit, COALESCE(sex,''), COALESCE(age_min,-1), COALESCE(age_max,-1))
			DO UPDATE SET low=EXCLUDED.low, high=EXCLUDED.high, source=EXCLUDED.source, knowledge_entry_id=EXCLUDED.knowledge_entry_id, approved_by=EXCLUDED.approved_by, approved_at=now()`,
			strings.ToUpper(r.Code), r.Unit, r.Sex, r.AgeMin, r.AgeMax, r.Low, r.High, r.Source, id, reviewer)
		return err
	case "unit_conversion":
		var c struct {
			Code     string   `json:"code"`
			FromUnit string   `json:"from_unit"`
			ToUnit   string   `json:"to_unit"`
			Factor   float64  `json:"factor"`
			Offset   *float64 `json:"offset"`
			Source   string   `json:"source"`
		}
		if err := json.Unmarshal(payload, &c); err != nil || c.Code == "" || c.FromUnit == "" || c.ToUnit == "" || c.FromUnit == c.ToUnit || c.Factor <= 0 || strings.TrimSpace(c.Source) == "" {
			return errors.New("invalid unit conversion entry")
		}
		offset := 0.0
		if c.Offset != nil {
			offset = *c.Offset
		}
		_, err := tx.Exec(ctx, `INSERT INTO unit_conversions(biomarker_code,from_unit,to_unit,factor,"offset",source,knowledge_entry_id,approved_by)
			VALUES($1::text,$2::text,$3::text,$4::float8,$5::float8,$6::text,$7::uuid,$8::text)
			ON CONFLICT (biomarker_code,from_unit,to_unit) DO UPDATE SET factor=EXCLUDED.factor,"offset"=EXCLUDED."offset",source=EXCLUDED.source,knowledge_entry_id=EXCLUDED.knowledge_entry_id,approved_by=EXCLUDED.approved_by,approved_at=now()`,
			strings.ToUpper(c.Code), c.FromUnit, c.ToUnit, c.Factor, offset, c.Source, id, reviewer)
		return err
	case "research_source":
		var s researchPayload
		if err := json.Unmarshal(payload, &s); err != nil {
			return err
		}
		var sourceID string
		if err := tx.QueryRow(ctx, `INSERT INTO research_sources(knowledge_entry_id,identifier,title,journal,published_year,url) VALUES($1::uuid,$2::text,$3::text,NULLIF($4::text,''),$5::int,$6::text)
			ON CONFLICT(knowledge_entry_id) DO UPDATE SET title=EXCLUDED.title RETURNING id::text`, id, s.Identifier, s.Title, s.Journal, s.Year, s.URL).Scan(&sourceID); err != nil {
			return err
		}
		for i, c := range chunks {
			if _, err := tx.Exec(ctx, `INSERT INTO research_chunks(source_id,ordinal,content,embedding,embedding_model) VALUES($1::uuid,$2::int,$3::text,$4::text::vector,$5::text)
				ON CONFLICT(source_id,ordinal) DO UPDATE SET content=EXCLUDED.content, embedding=EXCLUDED.embedding, embedding_model=EXCLUDED.embedding_model`, sourceID, i, c.text, embedding.VectorLiteral(c.vector), model); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("unsupported knowledge kind")
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// chunkAbstract splits on sentence boundaries into ~900 character passages, each prefixed by the title for retrieval context.
func chunkAbstract(title, abstract string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			out = append(out, title+"\n"+strings.TrimSpace(cur.String()))
		}
		cur.Reset()
	}
	for _, sentence := range strings.SplitAfter(abstract, ". ") {
		if cur.Len()+len(sentence) > 900 && cur.Len() > 0 {
			flush()
		}
		cur.WriteString(sentence)
	}
	flush()
	return out
}

func (m *Module) auditKnowledge(ctx context.Context, ownerID, action string, details map[string]string) {
	raw, _ := json.Marshal(details)
	_, _ = m.DB.Pool.Exec(ctx, `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1::text,$2::text,'knowledge',$3::text::jsonb)`, ownerID, action, string(raw))
}
