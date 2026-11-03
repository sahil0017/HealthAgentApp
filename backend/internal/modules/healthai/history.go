package healthai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxQuestionRunes = 4000

// Question is one saved question and the answer CORTEX gave, as returned by the history endpoints.
type Question struct {
	ID                 string          `json:"id"`
	Question           string          `json:"question"`
	Answer             string          `json:"answer"`
	Sources            json.RawMessage `json:"sources"`
	Limitations        json.RawMessage `json:"limitations"`
	Workflow           json.RawMessage `json:"workflow"`
	SuggestedQuestions json.RawMessage `json:"suggested_questions"`
	Urgent             bool            `json:"urgent"`
	Triage             string          `json:"triage"`
	Confidence         string          `json:"confidence"`
	Uncertainty        string          `json:"uncertainty"`
	Feedback           *string         `json:"feedback"`
	FeedbackComment    *string         `json:"feedback_comment"`
	CreatedAt          time.Time       `json:"created_at"`
}

const questionColumns = `id::text,question,answer,sources,limitations,workflow,suggested_questions,urgent,triage,confidence,uncertainty,feedback,feedback_comment,created_at`

type scanner interface{ Scan(dest ...any) error }

func scanQuestion(row scanner) (Question, error) {
	var q Question
	err := row.Scan(&q.ID, &q.Question, &q.Answer, &q.Sources, &q.Limitations, &q.Workflow, &q.SuggestedQuestions, &q.Urgent, &q.Triage, &q.Confidence, &q.Uncertainty, &q.Feedback, &q.FeedbackComment, &q.CreatedAt)
	return q, err
}

func jsonList(v any) []byte {
	if list, ok := v.([]any); ok {
		if b, err := json.Marshal(list); err == nil {
			return b
		}
	}
	return []byte("[]")
}

func oneOf(value any, fallback string, allowed ...string) string {
	text, _ := value.(string)
	if httpx.Contains(allowed, text) {
		return text
	}
	return fallback
}

// saveAnswer stores the question with its answer and the audit row in one transaction and returns the
// AI service answer extended with the saved id. Nothing is kept when either insert fails.
func (m *Module) saveAnswer(ctx context.Context, ownerID, profileID, question string, answer map[string]any, contextCount int) (map[string]any, error) {
	text, _ := answer["answer"].(string)
	urgent, _ := answer["urgent"].(bool)
	uncertainty, _ := answer["uncertainty"].(string)
	tx, err := m.DB.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id string
	var createdAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO health_questions(profile_id,owner_id,question,answer,sources,limitations,workflow,suggested_questions,urgent,triage,confidence,uncertainty)
		VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10,$11,$12) RETURNING id::text,created_at`,
		profileID, ownerID, question, text, jsonList(answer["sources"]), jsonList(answer["limitations"]), jsonList(answer["workflow"]), jsonList(answer["suggested_questions"]),
		urgent, oneOf(answer["triage"], "none", "none", "emergency", "crisis"),
		oneOf(answer["confidence"], "insufficient", "high", "limited", "insufficient", "not_applicable"), uncertainty).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'question.asked','agent_run',$3,jsonb_build_object('context_observation_count',$4::int,'urgent',$5::boolean))`, ownerID, profileID, id, contextCount, urgent); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	answer["id"], answer["question"], answer["created_at"] = id, question, createdAt
	return answer, nil
}

func (m *Module) listQuestions(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.NotFoundProfile(w)
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.Invalid(w, "INVALID_PAGINATION", "limit must be 1-100 and offset must be 0 or more.", "limit")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+questionColumns+` FROM health_questions WHERE profile_id=$1 AND owner_id=$2 ORDER BY created_at DESC, id LIMIT $3 OFFSET $4`, chi.URLParam(r, "profileID"), httpx.Owner(r), limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "QUESTIONS_UNAVAILABLE", "Question history could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			httpx.WriteError(w, 500, "QUESTIONS_UNAVAILABLE", "Question history could not be loaded.", nil)
			return
		}
		items = append(items, q)
	}
	if rows.Err() != nil {
		httpx.WriteError(w, 500, "QUESTIONS_UNAVAILABLE", "Question history could not be loaded.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"questions": items, "limit": limit, "offset": offset})
}

func questionNotFound(w http.ResponseWriter) {
	httpx.WriteError(w, 404, "QUESTION_NOT_FOUND", "The requested question could not be found.", nil)
}

func (m *Module) getQuestion(w http.ResponseWriter, r *http.Request) {
	id, ok := questionID(r)
	if !ok || !m.ProfileOwned(r) {
		questionNotFound(w)
		return
	}
	q, err := scanQuestion(m.DB.Pool.QueryRow(r.Context(), `SELECT `+questionColumns+` FROM health_questions WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, id, chi.URLParam(r, "profileID"), httpx.Owner(r)))
	if err != nil {
		questionNotFound(w)
		return
	}
	httpx.WriteJSON(w, 200, q)
}

func questionID(r *http.Request) (string, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "questionID"))
	return id.String(), err == nil
}

func (m *Module) rateQuestion(w http.ResponseWriter, r *http.Request) {
	id, ok := questionID(r)
	if !ok || !m.ProfileOwned(r) {
		questionNotFound(w)
		return
	}
	var in struct {
		Rating  string  `json:"rating"`
		Comment *string `json:"comment"`
	}
	if httpx.DecodeJSON(r, 8<<10, &in) != nil || !httpx.Contains([]string{"helpful", "not_helpful"}, in.Rating) {
		httpx.Invalid(w, "INVALID_RATING", "rating must be helpful or not_helpful.", "rating")
		return
	}
	var comment *string
	if in.Comment != nil {
		if trimmed := strings.TrimSpace(*in.Comment); trimmed != "" {
			if !httpx.ValidText(trimmed, 1000, false) {
				httpx.Invalid(w, "INVALID_COMMENT", "comment can be up to 1000 characters.", "comment")
				return
			}
			comment = &trimmed
		}
	}
	q, err := scanQuestion(m.DB.Pool.QueryRow(r.Context(), `UPDATE health_questions SET feedback=$4, feedback_comment=$5 WHERE id=$1 AND profile_id=$2 AND owner_id=$3 RETURNING `+questionColumns, id, chi.URLParam(r, "profileID"), httpx.Owner(r), in.Rating, comment))
	if err != nil {
		questionNotFound(w)
		return
	}
	httpx.WriteJSON(w, 200, q)
}

func (m *Module) deleteQuestion(w http.ResponseWriter, r *http.Request) {
	id, ok := questionID(r)
	if !ok || !m.ProfileOwned(r) {
		questionNotFound(w)
		return
	}
	tag, err := m.DB.Pool.Exec(r.Context(), `DELETE FROM health_questions WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, id, chi.URLParam(r, "profileID"), httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "QUESTION_DELETE_FAILED", "The question could not be deleted.", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		questionNotFound(w)
		return
	}
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'question.deleted','agent_run',$3)`, httpx.Owner(r), chi.URLParam(r, "profileID"), id)
	w.WriteHeader(http.StatusNoContent)
}
