package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/knowledge"
	"cortex/backend/internal/objects"
	"cortex/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type API struct {
	db        *store.Store
	devToken  string
	objects   *objects.Store
	knowledge *knowledge.Service
}

var errPagination = errors.New("invalid pagination")

func New(db *store.Store, objectStore *objects.Store, devToken string) http.Handler {
	a := &API{db: db, objects: objectStore, devToken: devToken, knowledge: knowledge.NewService(db)}
	r := chi.NewRouter()
	r.Use(requestContext)
	r.Use(accessLog)
	r.Use(recoverJSON)
	r.Get("/healthz", a.health)
	r.Get("/readyz", a.ready)
	for _, prefix := range []string{"/api/v1", "/v1"} {
		r.Route(prefix, func(r chi.Router) {
			r.Post("/auth/session", a.createSession)
			r.Post("/mcp", a.mcpEndpoint)
			r.Group(func(r chi.Router) {
				r.Use(a.authenticate)
				r.Post("/auth/logout", a.logout)
				r.Get("/users/me", a.currentUser)
				r.Get("/profiles", a.listProfiles)
				r.Post("/profiles", a.createProfile)
				r.Get("/profiles/{profileID}", a.getProfile)
				r.Patch("/profiles/{profileID}", a.updateProfile)
				r.Get("/profiles/{profileID}/consents", a.getConsents)
				r.Post("/profiles/{profileID}/consents", a.createConsent)
				r.Put("/profiles/{profileID}/consents/{purpose}", a.setConsent)
				r.Get("/consents/{consentID}", a.getConsent)
				r.Delete("/consents/{consentID}", a.withdrawConsent)
				r.Post("/profiles/{profileID}/documents", a.uploadDocument)
				r.Post("/profiles/{profileID}/documents/upload-intents", a.createUploadIntent)
				r.Get("/profiles/{profileID}/documents", a.listDocuments)
				r.Get("/profiles/{profileID}/documents/{documentID}", a.getDocument)
				r.Get("/profiles/{profileID}/documents/{documentID}/file", a.downloadDocument)
				r.Post("/profiles/{profileID}/documents/{documentID}/approve", a.approveDocument)
				r.Post("/profiles/{profileID}/documents/{documentID}/retry", a.retryDocument)
				r.Post("/documents/{documentID}/complete", a.completeUpload)
				r.Get("/documents/{documentID}", a.getDocumentByID)
				r.Get("/documents/{documentID}/versions", a.documentVersions)
				r.Get("/documents/{documentID}/extraction/pages", a.documentExtractionPages)
				r.Get("/documents/{documentID}/download", a.documentDownloadURL)
				r.Patch("/documents/{documentID}", a.updateDocument)
				r.Delete("/documents/{documentID}", a.deleteDocument)
				r.Get("/profiles/{profileID}/observations", a.listObservations)
				r.Post("/profiles/{profileID}/observations", a.createObservation)
				r.Get("/profiles/{profileID}/biomarkers", a.listBiomarkers)
				r.Get("/profiles/{profileID}/biomarkers/{code}/history", a.biomarkerHistory)
				r.Post("/observations/{observationID}/verify", a.verifyObservation)
				r.Post("/observations/{observationID}/corrections", a.correctObservation)
				r.Post("/profiles/{profileID}/questions", a.askQuestion)
				r.Post("/profiles/{profileID}/mcp-tokens", a.createMCPToken)
				r.Get("/knowledge/entries", a.listKnowledge)
				r.Post("/knowledge/research", a.requestResearch)
				r.Post("/knowledge/entries/bulk-approve", a.bulkApproveKnowledge)
				r.Post("/knowledge/entries/{entryID}/approve", a.approveKnowledge)
				r.Post("/knowledge/entries/{entryID}/reject", a.rejectKnowledge)
			})
		})
	}
	return r
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "cortex-api"})
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.db.Pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "dependency": "database"})
		return
	}
	if err := a.objects.Check(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "dependency": "object_storage"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "cortex-api"})
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "A valid bearer session is required.", nil)
			return
		}
		digest := sha256.Sum256([]byte(parts[1]))
		hash := hex.EncodeToString(digest[:])
		var userID string
		err := a.db.Pool.QueryRow(r.Context(), `SELECT s.user_id FROM api_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active'`, hash).Scan(&userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "The bearer session is expired or invalid.", nil)
			return
		}
		ctx := context.WithValue(r.Context(), ownerKey{}, userID)
		ctx = context.WithValue(ctx, sessionHashKey{}, hash)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ownerKey struct{}

func owner(r *http.Request) string { v, _ := r.Context().Value(ownerKey{}).(string); return v }

type profileInput struct {
	DisplayName string `json:"display_name"`
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var in profileInput
	if decodeJSON(r, 16<<10, &in) != nil || strings.TrimSpace(in.DisplayName) == "" || len(strings.TrimSpace(in.DisplayName)) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display_name_required"})
		return
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "profile_create_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var id, name string
	err = tx.QueryRow(r.Context(), `INSERT INTO profiles(owner_id, display_name) VALUES($1,$2) RETURNING id, display_name`, owner(r), strings.TrimSpace(in.DisplayName)).Scan(&id, &name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile_create_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'profile.created','profile',$2::uuid::text)`, owner(r), id)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "profile_create_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "display_name": name})
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := page(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_pagination"})
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id, display_name, created_at FROM profiles WHERE owner_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, owner(r), limit, offset)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "profile_list_failed"})
		return
	}
	defer rows.Close()
	type profile struct {
		ID          string    `json:"id"`
		DisplayName string    `json:"display_name"`
		CreatedAt   time.Time `json:"created_at"`
	}
	items := []profile{}
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.CreatedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "profile_list_failed"})
			return
		}
		items = append(items, p)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "profile_list_failed"})
		return
	}
	w.Header().Set("X-Page-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Page-Offset", strconv.Itoa(offset))
	writeJSON(w, 200, items)
}

func (a *API) profileOwned(r *http.Request) bool {
	var exists bool
	err := a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM profiles WHERE id=$1 AND owner_id=$2)`, chi.URLParam(r, "profileID"), owner(r)).Scan(&exists)
	return err == nil && exists
}

func (a *API) getConsents(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,purpose,granted,policy_version,recorded_at,expires_at,revoked_at FROM consents WHERE profile_id=$1 ORDER BY purpose`, chi.URLParam(r, "profileID"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "consent_read_failed"})
		return
	}
	defer rows.Close()
	type consent struct {
		ID            string     `json:"id"`
		Purpose       string     `json:"purpose"`
		Granted       bool       `json:"granted"`
		PolicyVersion string     `json:"policy_version"`
		RecordedAt    time.Time  `json:"recorded_at"`
		ExpiresAt     *time.Time `json:"expires_at,omitempty"`
		RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	}
	items := []consent{}
	for rows.Next() {
		var c consent
		if err := rows.Scan(&c.ID, &c.Purpose, &c.Granted, &c.PolicyVersion, &c.RecordedAt, &c.ExpiresAt, &c.RevokedAt); err != nil {
			writeJSON(w, 500, map[string]string{"error": "consent_read_failed"})
			return
		}
		items = append(items, c)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "consent_read_failed"})
		return
	}
	writeJSON(w, 200, items)
}

func (a *API) setConsent(w http.ResponseWriter, r *http.Request) {
	if !a.profileOwned(r) {
		writeJSON(w, 404, map[string]string{"error": "profile_not_found"})
		return
	}
	purpose := chi.URLParam(r, "purpose")
	if !validConsentPurpose(purpose) {
		writeJSON(w, 400, map[string]string{"error": "invalid_consent_purpose"})
		return
	}
	var in struct {
		Granted       *bool   `json:"granted"`
		PolicyVersion *string `json:"policy_version"`
	}
	if decodeJSON(r, 4096, &in) != nil || in.Granted == nil {
		writeJSON(w, 400, map[string]string{"error": "granted_required"})
		return
	}
	var recorded time.Time
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	defer tx.Rollback(r.Context())
	policyVersion := "1"
	if in.PolicyVersion != nil && strings.TrimSpace(*in.PolicyVersion) != "" {
		policyVersion = strings.TrimSpace(*in.PolicyVersion)
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO consents(profile_id,purpose,granted,policy_version,revoked_at) VALUES($1,$2,$3,$4,CASE WHEN $3 THEN NULL ELSE now() END) ON CONFLICT(profile_id,purpose) DO UPDATE SET granted=EXCLUDED.granted,policy_version=EXCLUDED.policy_version,revoked_at=EXCLUDED.revoked_at,recorded_at=now() RETURNING recorded_at`, chi.URLParam(r, "profileID"), purpose, *in.Granted, policyVersion).Scan(&recorded)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,details) VALUES($1,$2,'consent.changed','consent',jsonb_build_object('purpose',$3::text,'granted',$4::boolean))`, owner(r), chi.URLParam(r, "profileID"), purpose, *in.Granted)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "consent_write_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"purpose": purpose, "granted": *in.Granted, "recorded_at": recorded})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status >= 400 {
		code := "REQUEST_FAILED"
		if fields, ok := value.(map[string]string); ok && fields["error"] != "" {
			code = strings.ToUpper(fields["error"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": apiErrorMessage(code), "request_id": w.Header().Get("X-Request-ID"), "fields": map[string]string{}}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value, "meta": map[string]string{"request_id": w.Header().Get("X-Request-ID")}})
}

func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID"), "fields": fields}})
}

func apiErrorMessage(code string) string {
	messages := map[string]string{
		"PROFILE_NOT_FOUND":        "The requested profile could not be found.",
		"DOCUMENT_NOT_FOUND":       "The requested document could not be found.",
		"UNAUTHORIZED":             "A valid bearer session is required.",
		"INVALID_PAGINATION":       "Pagination parameters are invalid.",
		"INVALID_BIOMARKER_FIELDS": "Biomarker fields are invalid.",
	}
	if message := messages[code]; message != "" {
		return message
	}
	return "The request could not be completed."
}

func page(r *http.Request) (int, int, error) {
	limit, offset := 25, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, errPagination
		}
		limit = value
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1000000 {
			return 0, 0, errPagination
		}
		offset = value
	}
	return limit, offset, nil
}
