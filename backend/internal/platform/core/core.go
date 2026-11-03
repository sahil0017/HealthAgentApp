// Package core holds the shared runtime every module is built on: the database, object storage,
// the knowledge service, and the access guards (session, ownership, consent). Modules receive a
// *Core and must not reach around it.
package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"cortex/backend/internal/knowledge"
	"cortex/backend/internal/objects"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

// ObjectRemover deletes stored files; governance uses it, and tests substitute a fake.
type ObjectRemover interface {
	Remove(ctx context.Context, key string) error
}

type Core struct {
	DB        *store.Store
	Objects   *objects.Store
	Knowledge *knowledge.Service
	Remover   ObjectRemover
}

// New wires the shared runtime. objectStore may be nil in tests that never touch files.
func New(db *store.Store, objectStore *objects.Store) *Core {
	c := &Core{DB: db, Objects: objectStore, Knowledge: knowledge.NewService(db)}
	if objectStore != nil {
		c.Remover = objectStore
	}
	return c
}

// SessionCookie holds the web session. It is HttpOnly, so page scripts never see the token.
const SessionCookie = "cortex_session"

// CSRFHeader must accompany every state-changing request that authenticates with the cookie.
// Browsers cannot attach a custom header cross-site without a CORS preflight, which this API never grants.
const CSRFHeader = "X-Cortex-Client"

// Authenticate resolves the session (a bearer token for native apps, the session cookie for the web) to an owner.
func (c *Core) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if parts := strings.Fields(r.Header.Get("Authorization")); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = parts[1]
		} else if ck, err := r.Cookie(SessionCookie); err == nil && ck.Value != "" {
			token = ck.Value
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(CSRFHeader) == "" {
				httpx.WriteError(w, http.StatusForbidden, "CSRF_CHECK_FAILED", "The request is missing the client header.", nil)
				return
			}
		}
		if token == "" {
			httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "A valid session is required.", nil)
			return
		}
		digest := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(digest[:])
		var userID string
		err := c.DB.Pool.QueryRow(r.Context(), `SELECT s.user_id FROM api_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.status='active'`, hash).Scan(&userID)
		if err != nil {
			httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "The session is expired or invalid.", nil)
			return
		}
		next.ServeHTTP(w, r.WithContext(httpx.WithSession(r.Context(), userID, hash)))
	})
}

// ProfileOwned reports whether the {profileID} in the URL belongs to the authenticated owner.
func (c *Core) ProfileOwned(r *http.Request) bool {
	var exists bool
	err := c.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM profiles WHERE id=$1 AND owner_id=$2)`, chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&exists)
	return err == nil && exists
}

// HasConsent reports whether a purpose is currently granted for a profile.
func (c *Core) HasConsent(ctx context.Context, profileID, purpose string) bool {
	var granted bool
	err := c.DB.Pool.QueryRow(ctx, `SELECT granted FROM consents WHERE profile_id=$1 AND purpose=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, profileID, purpose).Scan(&granted)
	return err == nil && granted
}

// ProfileReadable writes a 404 and returns false unless the caller owns the profile.
func (c *Core) ProfileReadable(w http.ResponseWriter, r *http.Request) bool {
	if !c.ProfileOwned(r) {
		httpx.NotFoundProfile(w)
		return false
	}
	return true
}

// CanStore additionally requires consent to store health data, which every write needs.
func (c *Core) CanStore(w http.ResponseWriter, r *http.Request) bool {
	if !c.ProfileReadable(w, r) {
		return false
	}
	profileID := chi.URLParam(r, "profileID")
	if !c.HasConsent(r.Context(), profileID, "medical_record_storage") && !c.HasConsent(r.Context(), profileID, "health_data") {
		httpx.WriteError(w, http.StatusForbidden, "CONSENT_REQUIRED", "Storing health information requires your consent.", nil)
		return false
	}
	return true
}
