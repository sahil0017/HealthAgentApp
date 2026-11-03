package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	testIssuer   = "https://issuer.test"
	testAudience = "web-client"
)

type idp struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
	hits   atomic.Int32
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: k, kid: "k1"}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": p.kid, "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		}}})
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *idp) verifier() *Verifier {
	return NewVerifier([]Provider{{Name: "oidc", Issuers: []string{testIssuer}, JWKSURL: p.server.URL, Audiences: []string{testAudience}}})
}

// token signs claims with the IdP key; overrides replace or add claims.
func (p *idp) token(t *testing.T, overrides map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"iss": testIssuer, "sub": "user-1", "aud": testAudience, "nonce": "n-1",
		"exp": time.Now().Add(10 * time.Minute).Unix(), "iat": time.Now().Unix(),
	}
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	return p.sign(t, map[string]string{"alg": "RS256", "kid": p.kid, "typ": "JWT"}, claims, p.key)
}

func (p *idp) sign(t *testing.T, header map[string]string, claims map[string]any, key *rsa.PrivateKey) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifyAcceptsAValidToken(t *testing.T) {
	p := newIDP(t)
	c, err := p.verifier().Verify(context.Background(), "oidc", p.token(t, nil), "n-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID() != "oidc:user-1" {
		t.Fatalf("user id %q", c.UserID())
	}
}

func TestVerifyAcceptsTheHashedNonceAndAudienceArray(t *testing.T) {
	p := newIDP(t)
	sum := sha256.Sum256([]byte("raw-nonce"))
	tok := p.token(t, map[string]any{"nonce": hex.EncodeToString(sum[:]), "aud": []string{"other", testAudience}})
	if _, err := p.verifier().Verify(context.Background(), "oidc", tok, "raw-nonce"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	p := newIDP(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := map[string]struct {
		token, nonce string
	}{
		"wrong issuer":              {p.token(t, map[string]any{"iss": "https://evil.test"}), "n-1"},
		"wrong audience":            {p.token(t, map[string]any{"aud": "someone-else"}), "n-1"},
		"expired":                   {p.token(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}), "n-1"},
		"no expiry":                 {p.token(t, map[string]any{"exp": nil}), "n-1"},
		"issued in future":          {p.token(t, map[string]any{"iat": time.Now().Add(time.Hour).Unix()}), "n-1"},
		"missing subject":           {p.token(t, map[string]any{"sub": ""}), "n-1"},
		"nonce mismatch":            {p.token(t, nil), "different"},
		"missing nonce":             {p.token(t, nil), ""},
		"token without nonce claim": {p.token(t, map[string]any{"nonce": nil}), "n-1"},
		"unknown key id":            {p.sign(t, map[string]string{"alg": "RS256", "kid": "nope"}, map[string]any{"iss": testIssuer}, p.key), "n-1"},
		"alg none":                  {p.sign(t, map[string]string{"alg": "none", "kid": "k1"}, map[string]any{"iss": testIssuer}, p.key), "n-1"},
		"alg HS256":                 {p.sign(t, map[string]string{"alg": "HS256", "kid": "k1"}, map[string]any{"iss": testIssuer}, p.key), "n-1"},
		"signed by another key":     {p.sign(t, map[string]string{"alg": "RS256", "kid": "k1"}, map[string]any{"iss": testIssuer, "sub": "x", "aud": testAudience, "nonce": "n-1", "exp": time.Now().Add(time.Hour).Unix()}, other), "n-1"},
		"not a jwt":                 {"abc", "n-1"},
		"garbage header":            {"@@.@@.@@", "n-1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := p.verifier().Verify(context.Background(), "oidc", c.token, c.nonce)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("want ErrInvalidToken, got %v", err)
			}
		})
	}
}

func TestVerifyRejectsAnUnconfiguredProvider(t *testing.T) {
	p := newIDP(t)
	if _, err := p.verifier().Verify(context.Background(), "google", p.token(t, nil), "n-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("got %v", err)
	}
}

func TestSigningKeysAreCachedAndRefreshedOnRotation(t *testing.T) {
	p := newIDP(t)
	v := p.verifier()
	now := time.Now()
	v.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if _, err := v.Verify(context.Background(), "oidc", p.token(t, nil), "n-1"); err != nil {
			t.Fatal(err)
		}
	}
	if p.hits.Load() != 1 {
		t.Fatalf("keys fetched %d times, want 1", p.hits.Load())
	}
	// An unknown key id inside the retry window must not hammer the provider.
	bad := p.sign(t, map[string]string{"alg": "RS256", "kid": "rotated"}, map[string]any{}, p.key)
	_, _ = v.Verify(context.Background(), "oidc", bad, "n-1")
	if p.hits.Load() != 1 {
		t.Fatalf("refetched inside the retry window: %d", p.hits.Load())
	}
	// After the window the provider is asked again, so a rotated key is picked up.
	now = now.Add(2 * time.Minute)
	p.kid = "rotated"
	if _, err := v.Verify(context.Background(), "oidc", p.token(t, nil), "n-1"); err != nil {
		t.Fatal(err)
	}
	if p.hits.Load() != 2 {
		t.Fatalf("keys fetched %d times, want 2", p.hits.Load())
	}
}

func TestProviderKeysUnavailableIsNotAnInvalidToken(t *testing.T) {
	p := newIDP(t)
	v := p.verifier()
	p.server.Close()
	_, err := v.Verify(context.Background(), "oidc", p.token(t, nil), "n-1")
	if err == nil || errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want a provider-unavailable error, got %v", err)
	}
}

func TestLoadProviders(t *testing.T) {
	env := map[string]string{
		"CORTEX_GOOGLE_CLIENT_IDS":    "g1, g2",
		"CORTEX_GOOGLE_IOS_CLIENT_ID": "g-ios",
		"CORTEX_APPLE_CLIENT_IDS":     "com.example.app",
		"CORTEX_OIDC_ISSUER":          "https://idp.test",
		"CORTEX_OIDC_JWKS_URL":        "https://idp.test/keys",
		"CORTEX_OIDC_AUDIENCE":        "aud",
	}
	got := LoadProviders(func(k string) string { return env[k] })
	if len(got) != 3 || got[0].Name != "google" || len(got[0].Audiences) != 3 || got[0].NativeClientID != "g-ios" || got[1].Name != "apple" || got[2].Name != "oidc" {
		t.Fatalf("providers %+v", got)
	}
	if len(LoadProviders(func(string) string { return "" })) != 0 {
		t.Fatal("no configuration must mean no providers")
	}
	partial := map[string]string{"CORTEX_OIDC_ISSUER": "https://idp.test"}
	if len(LoadProviders(func(k string) string { return partial[k] })) != 0 {
		t.Fatal("a partly configured generic provider must be ignored")
	}
}

func router(t *testing.T, v *Verifier) (*Module, http.Handler) {
	t.Helper()
	m := New(testkit.Open(t))
	m.OIDC = v
	r := chi.NewRouter()
	m.Public(r)
	return m, r
}

func TestSessionEndpoint(t *testing.T) {
	p := newIDP(t)
	m, h := router(t, p.verifier())
	sub := "abc-" + uuid.NewString()
	uid := "oidc:" + sub
	body := func(tok, nonce string) string {
		b, _ := json.Marshal(map[string]string{"provider": "oidc", "id_token": tok, "nonce": nonce})
		return string(b)
	}

	res := testkit.Call(h, "POST", "/auth/session", body(p.token(t, map[string]any{"sub": sub}), "n-1"))
	if res.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", res.Code, res.Body)
	}
	var out struct {
		Data struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil || out.Data.AccessToken == "" || out.Data.TokenType != "Bearer" {
		t.Fatalf("bad session body: %s", res.Body)
	}
	if n := testkit.Count(t, m.Core, `SELECT count(*) FROM users WHERE id=$1`, uid); n != 1 {
		t.Fatalf("user rows %d", n)
	}
	// A second sign-in reuses the same account.
	testkit.Call(h, "POST", "/auth/session", body(p.token(t, map[string]any{"sub": sub}), "n-1"))
	if n := testkit.Count(t, m.Core, `SELECT count(*) FROM users WHERE id=$1`, uid); n != 1 {
		t.Fatalf("duplicate user rows %d", n)
	}
	if n := testkit.Count(t, m.Core, `SELECT count(*) FROM api_sessions WHERE user_id=$1`, uid); n != 2 {
		t.Fatalf("sessions %d", n)
	}
	// The raw access token is never stored.
	if n := testkit.Count(t, m.Core, `SELECT count(*) FROM api_sessions WHERE token_hash=$1`, out.Data.AccessToken); n != 0 {
		t.Fatal("raw token stored")
	}

	for name, b := range map[string]string{
		"bad token":     body("x.y.z", "n-1"),
		"wrong nonce":   body(p.token(t, nil), "nope"),
		"no body":       "",
		"unknown field": `{"provider":"oidc","id_token":"a","nonce":"b","admin":true}`,
	} {
		if c := testkit.Call(h, "POST", "/auth/session", b).Code; c != http.StatusUnauthorized && c != http.StatusBadRequest {
			t.Fatalf("%s: status %d", name, c)
		}
	}
	if !strings.Contains(testkit.Call(h, "POST", "/auth/session", body("x.y.z", "n")).Body.String(), "UNAUTHORIZED") {
		t.Fatal("expected UNAUTHORIZED code")
	}
}

func TestSessionEndpointWithoutProviders(t *testing.T) {
	_, h := router(t, NewVerifier(nil))
	if c := testkit.Call(h, "POST", "/auth/session", `{"provider":"google","id_token":"a","nonce":"b"}`).Code; c != http.StatusServiceUnavailable {
		t.Fatalf("status %d", c)
	}
}

func TestAuthConfigListsOnlyBrowserProviders(t *testing.T) {
	v := NewVerifier([]Provider{
		{Name: "google", Audiences: []string{"g-web", "g-ios"}, NativeClientID: "g-ios"},
		{Name: "apple", Audiences: []string{"svc.id"}},
		{Name: "oidc", Audiences: []string{"internal"}},
	})
	_, h := router(t, v)
	res := testkit.Call(h, "GET", "/auth/config", "")
	var out struct {
		Data struct {
			Providers []providerInfo `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := out.Data.Providers
	if len(got) != 2 || got[0] != (providerInfo{"google", "g-web", "g-ios"}) || got[1] != (providerInfo{"apple", "svc.id", ""}) {
		t.Fatalf("providers %+v", got)
	}
}

func TestWebSessionCookie(t *testing.T) {
	p := newIDP(t)
	m := New(testkit.Open(t))
	m.OIDC = p.verifier()
	r := chi.NewRouter()
	m.Public(r)
	r.Group(func(r chi.Router) {
		r.Use(m.Authenticate)
		m.Authed(r)
	})

	b, _ := json.Marshal(map[string]string{"provider": "oidc", "id_token": p.token(t, map[string]any{"sub": "web-" + uuid.NewString()}), "nonce": "n-1"})
	res := testkit.Call(r, "POST", "/auth/session", string(b))
	var cookie *http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == core.SessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == "" {
		t.Fatalf("session cookie missing or weak: %+v", cookie)
	}

	do := func(method, path string, headers map[string]string, withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if withCookie {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if c := do("GET", "/users/me", nil, true).Code; c != 200 {
		t.Fatalf("cookie read: %d", c)
	}
	if c := do("GET", "/users/me", nil, false).Code; c != 401 {
		t.Fatalf("no credential: %d", c)
	}
	if c := do("GET", "/users/me", map[string]string{"Authorization": "Bearer " + cookie.Value}, false).Code; c != 200 {
		t.Fatalf("bearer still works for native apps: %d", c)
	}
	if c := do("POST", "/auth/logout", nil, true).Code; c != 403 {
		t.Fatalf("cookie write without client header must be refused, got %d", c)
	}
	out := do("POST", "/auth/logout", map[string]string{core.CSRFHeader: "web"}, true)
	if out.Code != 200 {
		t.Fatalf("logout: %d", out.Code)
	}
	cleared := false
	for _, c := range out.Result().Cookies() {
		cleared = cleared || (c.Name == core.SessionCookie && c.MaxAge < 0)
	}
	if !cleared {
		t.Fatal("logout must clear the cookie")
	}
	if c := do("GET", "/users/me", nil, true).Code; c != 401 {
		t.Fatalf("revoked session still accepted: %d", c)
	}
}
