package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"
)

type sessionHashKey struct{}

type sessionResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (a *API) createSession(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv("CORTEX_AUTH_MODE")) != "development" || a.devToken == "" {
		writeError(w, http.StatusServiceUnavailable, "AUTH_PROVIDER_NOT_CONFIGURED", "An OIDC identity provider is not configured.", nil)
		return
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(a.devToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "The development credential is invalid.", nil)
		return
	}
	userID := strings.TrimSpace(os.Getenv("CORTEX_DEV_OWNER_ID"))
	if userID == "" {
		userID = "local-development-user"
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(id) VALUES($1) ON CONFLICT(id) DO UPDATE SET updated_at=now() WHERE users.status='active' RETURNING status`, userID).Scan(&userStatus)
	if err != nil || userStatus != "active" {
		writeError(w, 403, "ACCOUNT_UNAVAILABLE", "This account is unavailable.", nil)
		return
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		writeError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	digest := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(digest[:])
	expires := time.Now().UTC().Add(8 * time.Hour)
	if _, err := tx.Exec(r.Context(), `INSERT INTO api_sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, tokenHash, userID, expires); err != nil {
		writeError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,'auth.session_created','session','{}'::jsonb)`, userID); err != nil {
		writeError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	writeJSON(w, http.StatusCreated, sessionResponse{AccessToken: token, TokenType: "Bearer", ExpiresAt: expires})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	hash, _ := r.Context().Value(sessionHashKey{}).(string)
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "LOGOUT_FAILED", "The session could not be revoked.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE api_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE token_hash=$1`, hash)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,'auth.session_revoked','session','{}'::jsonb)`, owner(r))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "LOGOUT_FAILED", "The session could not be revoked.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (a *API) currentUser(w http.ResponseWriter, r *http.Request) {
	var user struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	err := a.db.Pool.QueryRow(r.Context(), `SELECT id,status,created_at FROM users WHERE id=$1`, owner(r)).Scan(&user.ID, &user.Status, &user.CreatedAt)
	if err != nil {
		writeError(w, 404, "USER_NOT_FOUND", "The authenticated user was not found.", nil)
		return
	}
	writeJSON(w, http.StatusOK, user)
}
