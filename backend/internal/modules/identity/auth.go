package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type sessionHashKey struct{}

const (
	sessionTTL = 8 * time.Hour
	// A session can be refreshed for up to this long after the original sign-in; then the user signs in again.
	maxSessionAge = 30 * 24 * time.Hour
)

func newToken() (token, hash string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

// deviceOf names the client for the device list. Only a short label and a truncated user agent are kept.
func deviceOf(r *http.Request) (device, agent string) {
	device = "api"
	if c := strings.ToLower(strings.TrimSpace(r.Header.Get(core.CSRFHeader))); c == "web" || c == "ios" || c == "android" {
		device = c
	}
	agent = r.UserAgent()
	if len(agent) > 120 {
		agent = agent[:120]
	}
	return device, agent
}

type sessionResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type sessionRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"id_token"`
	Nonce    string `json:"nonce"`
}

// createSession exchanges a verified Google or Apple ID token for a CORTEX session.
func (m *Module) createSession(w http.ResponseWriter, r *http.Request) {
	if len(m.OIDC.Providers()) == 0 {
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_PROVIDER_NOT_CONFIGURED", "No identity provider is configured.", nil)
		return
	}
	var in sessionRequest
	if httpx.DecodeJSON(r, 16<<10, &in) != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "provider, id_token and nonce are required.", nil)
		return
	}
	claims, err := m.OIDC.Verify(r.Context(), in.Provider, in.IDToken, in.Nonce)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "The sign-in could not be verified.", nil)
		} else {
			httpx.WriteError(w, http.StatusBadGateway, "IDENTITY_PROVIDER_UNAVAILABLE", "The identity provider could not be reached. Try again.", nil)
		}
		return
	}
	userID := claims.UserID()
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(id) VALUES($1) ON CONFLICT(id) DO UPDATE SET updated_at=now() WHERE users.status='active' RETURNING status`, userID).Scan(&userStatus)
	if err != nil || userStatus != "active" {
		httpx.WriteError(w, 403, "ACCOUNT_UNAVAILABLE", "This account is unavailable.", nil)
		return
	}
	token, tokenHash, err := newToken()
	if err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	expires := time.Now().UTC().Add(sessionTTL)
	device, agent := deviceOf(r)
	if _, err := tx.Exec(r.Context(), `INSERT INTO api_sessions(token_hash,user_id,expires_at,device,user_agent) VALUES($1,$2,$3,$4,$5)`, tokenHash, userID, expires, device, agent); err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,'auth.session_created','session','{}'::jsonb)`, userID); err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	http.SetCookie(w, sessionCookie(r, token, expires))
	httpx.WriteJSON(w, http.StatusCreated, sessionResponse{AccessToken: token, TokenType: "Bearer", ExpiresAt: expires})
}

// sessionCookie carries the session for browsers. Secure is set whenever the request arrived over HTTPS,
// directly or through a TLS-terminating proxy.
func sessionCookie(r *http.Request, value string, expires time.Time) *http.Cookie {
	c := &http.Cookie{
		Name: core.SessionCookie, Value: value, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	hash := httpx.SessionHash(r)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "LOGOUT_FAILED", "The session could not be revoked.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `UPDATE api_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE token_hash=$1`, hash)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,'auth.session_revoked','session','{}'::jsonb)`, httpx.Owner(r))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "LOGOUT_FAILED", "The session could not be revoked.", nil)
		return
	}
	http.SetCookie(w, sessionCookie(r, "", time.Unix(0, 0)))
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (m *Module) currentUser(w http.ResponseWriter, r *http.Request) {
	var user struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	err := m.DB.Pool.QueryRow(r.Context(), `SELECT id,status,created_at FROM users WHERE id=$1`, httpx.Owner(r)).Scan(&user.ID, &user.Status, &user.CreatedAt)
	if err != nil {
		httpx.WriteError(w, 404, "USER_NOT_FOUND", "The authenticated user was not found.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

type providerInfo struct {
	Name     string `json:"name"`
	ClientID string `json:"client_id"`
	// IOSClientID is set only where the native app needs a different client than the browser (Google).
	IOSClientID string `json:"ios_client_id,omitempty"`
}

// authConfig tells a client which sign-in providers are available and the client ID to start each with.
func (m *Module) authConfig(w http.ResponseWriter, r *http.Request) {
	out := []providerInfo{}
	for _, p := range m.OIDC.Providers() {
		if p.Name == "google" || p.Name == "apple" {
			out = append(out, providerInfo{Name: p.Name, ClientID: p.Audiences[0], IOSClientID: p.NativeClientID})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// refresh rotates the current session: a new token with a fresh expiry replaces the old one, which stops working.
func (m *Module) refresh(w http.ResponseWriter, r *http.Request) {
	token, hash, err := newToken()
	if err != nil {
		httpx.WriteError(w, 500, "REFRESH_FAILED", "The session could not be refreshed.", nil)
		return
	}
	expires := time.Now().UTC().Add(sessionTTL)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "REFRESH_FAILED", "The session could not be refreshed.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var started time.Time
	var device, agent string
	err = tx.QueryRow(r.Context(), `UPDATE api_sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL RETURNING started_at,device,user_agent`, httpx.SessionHash(r)).Scan(&started, &device, &agent)
	if err != nil || time.Since(started) > maxSessionAge {
		httpx.WriteError(w, http.StatusUnauthorized, "SESSION_TOO_OLD", "Please sign in again.", nil)
		return
	}
	// The cap is on the original sign-in, so the new session must not outlive it.
	if limit := started.Add(maxSessionAge); expires.After(limit) {
		expires = limit
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO api_sessions(token_hash,user_id,expires_at,device,user_agent,started_at) VALUES($1,$2,$3,$4,$5,$6)`, hash, httpx.Owner(r), expires, device, agent, started); err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,'auth.session_refreshed','session','{}'::jsonb)`, httpx.Owner(r))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "REFRESH_FAILED", "The session could not be refreshed.", nil)
		return
	}
	// Only browsers hold the cookie; a native client reads the token from the body.
	if _, cerr := r.Cookie(core.SessionCookie); cerr == nil {
		http.SetCookie(w, sessionCookie(r, token, expires))
	}
	httpx.WriteJSON(w, http.StatusOK, sessionResponse{AccessToken: token, TokenType: "Bearer", ExpiresAt: expires})
}

type deviceSession struct {
	ID         string    `json:"id"`
	Device     string    `json:"device"`
	UserAgent  string    `json:"user_agent"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (m *Module) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text,device,user_agent,started_at,last_seen_at,expires_at,token_hash=$2
		FROM api_sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now() ORDER BY last_seen_at DESC LIMIT 50`, httpx.Owner(r), httpx.SessionHash(r))
	if err != nil {
		httpx.WriteError(w, 500, "SESSIONS_UNAVAILABLE", "Sessions could not be listed.", nil)
		return
	}
	defer rows.Close()
	out := []deviceSession{}
	for rows.Next() {
		var s deviceSession
		if err := rows.Scan(&s.ID, &s.Device, &s.UserAgent, &s.StartedAt, &s.LastSeenAt, &s.ExpiresAt, &s.Current); err != nil {
			httpx.WriteError(w, 500, "SESSIONS_UNAVAILABLE", "Sessions could not be listed.", nil)
			return
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (m *Module) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(id); err != nil {
		httpx.WriteError(w, 400, "INVALID_SESSION_ID", "The session id is invalid.", nil)
		return
	}
	tag, err := m.DB.Pool.Exec(r.Context(), `UPDATE api_sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "REVOKE_FAILED", "The session could not be revoked.", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, 404, "SESSION_NOT_FOUND", "The session was not found.", nil)
		return
	}
	m.audit(r, "auth.session_revoked")
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// logoutEverywhere revokes every session of the user, including this one.
func (m *Module) logoutEverywhere(w http.ResponseWriter, r *http.Request) {
	tag, err := m.DB.Pool.Exec(r.Context(), `UPDATE api_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "REVOKE_FAILED", "Sessions could not be revoked.", nil)
		return
	}
	m.audit(r, "auth.all_sessions_revoked")
	http.SetCookie(w, sessionCookie(r, "", time.Unix(0, 0)))
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"revoked": tag.RowsAffected()})
}

func (m *Module) audit(r *http.Request, action string) {
	_, _ = m.DB.Pool.Exec(r.Context(), `INSERT INTO audit_events(owner_id,action,resource_type,details) VALUES($1,$2,'session','{}'::jsonb)`, httpx.Owner(r), action)
}
