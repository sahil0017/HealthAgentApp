package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"
)

type sessionHashKey struct{}

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
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		httpx.WriteError(w, 500, "SESSION_CREATE_FAILED", "The session could not be created.", nil)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	digest := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(digest[:])
	expires := time.Now().UTC().Add(8 * time.Hour)
	if _, err := tx.Exec(r.Context(), `INSERT INTO api_sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, tokenHash, userID, expires); err != nil {
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
