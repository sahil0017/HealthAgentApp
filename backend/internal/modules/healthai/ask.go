package healthai

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type answerRequest struct {
	Question string `json:"question"`
}

func (m *Module) askQuestion(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !m.ProfileOwned(r) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	if !m.HasConsent(r.Context(), profileID, "medical_record_storage") && !m.HasConsent(r.Context(), profileID, "health_data") {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "Health data consent is required.", nil)
		return
	}
	if !m.HasConsent(r.Context(), profileID, "ai_processing") {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "AI processing consent is required.", nil)
		return
	}
	var in answerRequest
	if httpx.DecodeJSON(r, 32<<10, &in) != nil || strings.TrimSpace(in.Question) == "" {
		httpx.WriteJSON(w, 400, map[string]string{"error": "question_required"})
		return
	}
	in.Question = strings.TrimSpace(in.Question)
	if utf8.RuneCountInString(in.Question) > maxQuestionRunes {
		httpx.Invalid(w, "QUESTION_TOO_LONG", "Questions can be up to 4000 characters.", "question")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT name,value::float8,unit,observed_at,COALESCE(reference_range,'') FROM observations WHERE profile_id=$1 AND owner_id=$2 AND verification_status='VERIFIED' ORDER BY name,observed_at DESC NULLS LAST LIMIT 500`, profileID, httpx.Owner(r))
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "context_unavailable"})
		return
	}
	type item struct {
		Name           string     `json:"name"`
		Value          float64    `json:"value"`
		Unit           string     `json:"unit"`
		ObservedAt     *time.Time `json:"observed_at,omitempty"`
		ReferenceRange string     `json:"reference_range,omitempty"`
	}
	contextItems := []item{}
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.Name, &v.Value, &v.Unit, &v.ObservedAt, &v.ReferenceRange); err != nil {
			rows.Close()
			httpx.WriteJSON(w, 500, map[string]string{"error": "context_unavailable"})
			return
		}
		contextItems = append(contextItems, v)
	}
	if rows.Err() != nil {
		rows.Close()
		httpx.WriteJSON(w, 500, map[string]string{"error": "context_unavailable"})
		return
	}
	rows.Close()
	chunks := []recordHit{}
	if len(contextItems) > 0 {
		if err := m.indexProfileRecords(r.Context(), httpx.Owner(r), profileID); err != nil {
			log.Printf("record indexing unavailable: %v", err)
		} else if hits, err := m.searchRecords(r.Context(), httpx.Owner(r), profileID, strings.TrimSpace(in.Question), 5); err != nil {
			log.Printf("record search unavailable: %v", err)
		} else {
			chunks = hits
		}
	}
	research, err := m.searchResearch(r.Context(), strings.TrimSpace(in.Question), 3)
	if err != nil {
		log.Printf("research search unavailable: %v", err)
		research = []researchHit{}
	}
	// The AI service rejects null lists, which a search with no hits would otherwise send.
	if research == nil {
		research = []researchHit{}
	}
	if chunks == nil {
		chunks = []recordHit{}
	}
	body, _ := json.Marshal(map[string]any{"question": strings.TrimSpace(in.Question), "observations": contextItems, "record_chunks": chunks, "research_chunks": research})
	service := embedding.ServiceURL()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, service+"/v1/answer", bytes.NewReader(body))
	if err != nil {
		httpx.WriteJSON(w, 502, map[string]string{"error": "ai_service_unavailable"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httpx.WriteJSON(w, 502, map[string]string{"error": "ai_service_unavailable"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "ai_service_error"})
		return
	}
	var answer map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "ai_service_error"})
		return
	}
	saved, err := m.saveAnswer(r.Context(), httpx.Owner(r), profileID, in.Question, answer, len(contextItems))
	if err != nil {
		log.Printf("saving answer failed: %v", err)
		httpx.WriteJSON(w, 500, map[string]string{"error": "audit_write_failed"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, saved)
}
