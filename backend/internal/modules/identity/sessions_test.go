package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type sessionEnv struct {
	t *testing.T
	m *Module
	h http.Handler
	p *idp
}

func newSessionEnv(t *testing.T) *sessionEnv {
	p := newIDP(t)
	m := New(testkit.Open(t))
	m.OIDC = p.verifier()
	r := chi.NewRouter()
	m.Public(r)
	r.Group(func(r chi.Router) {
		r.Use(m.Authenticate)
		m.Authed(r)
	})
	return &sessionEnv{t, m, r, p}
}

func sha256Hex(token string) string {
	d := sha256.Sum256([]byte(token))
	return hex.EncodeToString(d[:])
}

// signIn creates a session for sub from the named client and returns its bearer token.
func (e *sessionEnv) signIn(sub, client string) string {
	e.t.Helper()
	b, _ := json.Marshal(map[string]string{"provider": "oidc", "id_token": e.p.token(e.t, map[string]any{"sub": sub}), "nonce": "n-1"})
	req := httptest.NewRequest("POST", "/auth/session", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	if client != "" {
		req.Header.Set(core.CSRFHeader, client)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out struct {
		Data sessionResponse `json:"data"`
	}
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("sign in: %d %s", rec.Code, rec.Body)
	}
	return out.Data.AccessToken
}

func (e *sessionEnv) call(token, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *sessionEnv) sessions(token string) []deviceSession {
	e.t.Helper()
	rec := e.call(token, "GET", "/auth/sessions")
	var out struct {
		Data []deviceSession `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	return out.Data
}

func TestRefreshRotatesTheToken(t *testing.T) {
	e := newSessionEnv(t)
	old := e.signIn("rot-"+uuid.NewString(), "ios")
	rec := e.call(old, "POST", "/auth/refresh")
	var out struct {
		Data sessionResponse `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Data.AccessToken == "" || out.Data.AccessToken == old {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	if c := e.call(old, "GET", "/users/me").Code; c != 401 {
		t.Fatalf("the old token must stop working, got %d", c)
	}
	if c := e.call(out.Data.AccessToken, "GET", "/users/me").Code; c != 200 {
		t.Fatalf("the new token must work, got %d", c)
	}
	if c := e.call(old, "POST", "/auth/refresh").Code; c != 401 {
		t.Fatalf("a rotated token cannot be refreshed again, got %d", c)
	}
	s := e.sessions(out.Data.AccessToken)
	if len(s) != 1 || s[0].Device != "ios" || !s[0].Current {
		t.Fatalf("refresh must keep one device session: %+v", s)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == core.SessionCookie {
			t.Fatal("a bearer client must not be given a cookie")
		}
	}
}

func TestRefreshIsCappedByTheOriginalSignIn(t *testing.T) {
	e := newSessionEnv(t)
	token := e.signIn("cap-"+uuid.NewString(), "web")
	if _, err := e.m.DB.Pool.Exec(t.Context(), `UPDATE api_sessions SET started_at=now()-interval '31 days' WHERE token_hash=$1`, sha256Hex(token)); err != nil {
		t.Fatal(err)
	}
	if c := e.call(token, "POST", "/auth/refresh").Code; c != 401 {
		t.Fatalf("a session older than the cap must require sign-in, got %d", c)
	}

	token = e.signIn("cap-"+uuid.NewString(), "web")
	if _, err := e.m.DB.Pool.Exec(t.Context(), `UPDATE api_sessions SET started_at=now()-interval '29 days 23 hours' WHERE token_hash=$1`, sha256Hex(token)); err != nil {
		t.Fatal(err)
	}
	rec := e.call(token, "POST", "/auth/refresh")
	var out struct {
		Data sessionResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Data.ExpiresAt.After(time.Now().Add(61*time.Minute)) {
		t.Fatalf("expiry must not outlive the 30-day cap: %d %v", rec.Code, out.Data.ExpiresAt)
	}
}

func TestWebRefreshReplacesTheCookie(t *testing.T) {
	e := newSessionEnv(t)
	token := e.signIn("cook-"+uuid.NewString(), "web")
	req := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(core.CSRFHeader, "web")
	req.AddCookie(&http.Cookie{Name: core.SessionCookie, Value: token})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	var fresh *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == core.SessionCookie {
			fresh = c
		}
	}
	if fresh == nil || fresh.Value == "" || fresh.Value == token || !fresh.HttpOnly {
		t.Fatalf("expected a new HttpOnly cookie, got %+v", fresh)
	}
}

func TestDeviceSessionsListAndRevoke(t *testing.T) {
	e := newSessionEnv(t)
	sub := "dev-" + uuid.NewString()
	web := e.signIn(sub, "web")
	ios := e.signIn(sub, "ios")
	stranger := e.signIn("other-"+uuid.NewString(), "web")

	list := e.sessions(web)
	if len(list) != 2 {
		t.Fatalf("want two devices, got %+v", list)
	}
	var current, other deviceSession
	for _, s := range list {
		if s.Current {
			current = s
		} else {
			other = s
		}
	}
	if current.Device != "web" || other.Device != "ios" {
		t.Fatalf("devices mislabelled: %+v", list)
	}

	if c := e.call(stranger, "DELETE", "/auth/sessions/"+other.ID).Code; c != 404 {
		t.Fatalf("another user's session must look absent, got %d", c)
	}
	if c := e.call(ios, "GET", "/users/me").Code; c != 200 {
		t.Fatal("a stranger's attempt must not revoke the session")
	}
	if c := e.call(web, "DELETE", "/auth/sessions/not-a-uuid").Code; c != 400 {
		t.Fatalf("bad id: %d", c)
	}
	if c := e.call(web, "DELETE", "/auth/sessions/"+other.ID).Code; c != 200 {
		t.Fatalf("revoke: %d", c)
	}
	if c := e.call(ios, "GET", "/users/me").Code; c != 401 {
		t.Fatalf("the revoked device must be signed out, got %d", c)
	}
	if c := e.call(web, "DELETE", "/auth/sessions/"+other.ID).Code; c != 404 {
		t.Fatalf("revoking twice must report not found, got %d", c)
	}
	if len(e.sessions(web)) != 1 {
		t.Fatal("only the current session should remain")
	}
}

func TestLogoutEverywhere(t *testing.T) {
	e := newSessionEnv(t)
	sub := "all-" + uuid.NewString()
	a, b := e.signIn(sub, "web"), e.signIn(sub, "ios")
	other := e.signIn("keep-"+uuid.NewString(), "web")

	rec := e.call(a, "POST", "/auth/logout-all")
	var out struct {
		Data map[string]int64 `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Data["revoked"] != 2 {
		t.Fatalf("logout-all: %d %s", rec.Code, rec.Body)
	}
	for _, tok := range []string{a, b} {
		if c := e.call(tok, "GET", "/users/me").Code; c != 401 {
			t.Fatalf("every session of the user must end, got %d", c)
		}
	}
	if c := e.call(other, "GET", "/users/me").Code; c != 200 {
		t.Fatal("other users must be unaffected")
	}
}

func TestSessionActivityIsRecorded(t *testing.T) {
	e := newSessionEnv(t)
	token := e.signIn("seen-"+uuid.NewString(), "ios")
	if _, err := e.m.DB.Pool.Exec(t.Context(), `UPDATE api_sessions SET last_seen_at=now()-interval '1 hour' WHERE token_hash=$1`, sha256Hex(token)); err != nil {
		t.Fatal(err)
	}
	e.call(token, "GET", "/users/me")
	if s := e.sessions(token); len(s) != 1 || time.Since(s[0].LastSeenAt) > time.Minute {
		t.Fatalf("last seen was not refreshed: %+v", s)
	}
}

func TestAndroidClientGetsABearerSessionLabelledAndroid(t *testing.T) {
	e := newSessionEnv(t)
	token := e.signIn("and-"+uuid.NewString(), "android")
	s := e.sessions(token)
	if len(s) != 1 || s[0].Device != "android" {
		t.Fatalf("android must be recorded as its own device: %+v", s)
	}
}
