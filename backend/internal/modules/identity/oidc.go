package identity

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Provider describes one OpenID Connect identity provider whose ID tokens CORTEX accepts.
type Provider struct {
	Name      string
	Issuers   []string
	JWKSURL   string
	Audiences []string
}

// Claims are the verified facts CORTEX keeps from an ID token. Email and name are never read.
type Claims struct {
	Provider string
	Subject  string
}

// UserID is the stable owner ID for a person: the provider plus the provider's subject.
func (c Claims) UserID() string { return c.Provider + ":" + c.Subject }

var ErrInvalidToken = errors.New("invalid identity token")

const (
	clockSkew     = 2 * time.Minute
	jwksTTL       = time.Hour
	jwksRetryWait = time.Minute
	maxJWKSBytes  = 256 << 10
)

// Verifier checks ID tokens against each provider's published signing keys.
type Verifier struct {
	providers map[string]Provider
	client    *http.Client
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]*keySet
}

type keySet struct {
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func NewVerifier(providers []Provider) *Verifier {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Name] = p
	}
	return &Verifier{
		providers: m,
		client:    &http.Client{Timeout: 5 * time.Second},
		now:       time.Now,
		cache:     map[string]*keySet{},
	}
}

// Providers returns the configured providers in a stable order.
func (v *Verifier) Providers() []Provider {
	var out []Provider
	for _, name := range []string{"google", "apple", "oidc"} {
		if p, ok := v.providers[name]; ok {
			out = append(out, p)
		}
	}
	return out
}

// LoadProviders reads the provider configuration from environment values.
//
//	CORTEX_GOOGLE_CLIENT_IDS, CORTEX_APPLE_CLIENT_IDS: comma-separated OAuth client IDs (token audiences)
//	CORTEX_OIDC_ISSUER, CORTEX_OIDC_JWKS_URL, CORTEX_OIDC_AUDIENCE: one additional generic OIDC provider
func LoadProviders(getenv func(string) string) []Provider {
	var out []Provider
	if ids := splitList(getenv("CORTEX_GOOGLE_CLIENT_IDS")); len(ids) > 0 {
		out = append(out, Provider{
			Name:      "google",
			Issuers:   []string{"https://accounts.google.com", "accounts.google.com"},
			JWKSURL:   "https://www.googleapis.com/oauth2/v3/certs",
			Audiences: ids,
		})
	}
	if ids := splitList(getenv("CORTEX_APPLE_CLIENT_IDS")); len(ids) > 0 {
		out = append(out, Provider{
			Name:      "apple",
			Issuers:   []string{"https://appleid.apple.com"},
			JWKSURL:   "https://appleid.apple.com/auth/keys",
			Audiences: ids,
		})
	}
	issuer, jwks, aud := strings.TrimSpace(getenv("CORTEX_OIDC_ISSUER")), strings.TrimSpace(getenv("CORTEX_OIDC_JWKS_URL")), splitList(getenv("CORTEX_OIDC_AUDIENCE"))
	if issuer != "" && jwks != "" && len(aud) > 0 {
		out = append(out, Provider{Name: "oidc", Issuers: []string{issuer}, JWKSURL: jwks, Audiences: aud})
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Issuer   string          `json:"iss"`
	Subject  string          `json:"sub"`
	Audience json.RawMessage `json:"aud"`
	Expires  int64           `json:"exp"`
	IssuedAt int64           `json:"iat"`
	Nonce    string          `json:"nonce"`
}

// Verify validates the signature, issuer, audience, lifetime and nonce of an ID token.
// The nonce binds the token to the sign-in attempt that requested it, so a stolen token cannot be replayed.
// Providers differ on whether they echo the raw nonce or its SHA-256 hex digest, so both are accepted.
func (v *Verifier) Verify(ctx context.Context, provider, idToken, nonce string) (Claims, error) {
	p, ok := v.providers[provider]
	if !ok {
		return Claims{}, fmt.Errorf("%w: provider %q is not configured", ErrInvalidToken, provider)
	}
	if strings.TrimSpace(nonce) == "" {
		return Claims{}, fmt.Errorf("%w: nonce is required", ErrInvalidToken)
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: malformed token", ErrInvalidToken)
	}
	var h jwtHeader
	if err := decodePart(parts[0], &h); err != nil {
		return Claims{}, fmt.Errorf("%w: bad header", ErrInvalidToken)
	}
	if h.Alg != "RS256" {
		return Claims{}, fmt.Errorf("%w: unsupported algorithm", ErrInvalidToken)
	}
	key, err := v.key(ctx, p, h.Kid)
	if err != nil {
		return Claims{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: bad signature encoding", ErrInvalidToken)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return Claims{}, fmt.Errorf("%w: signature mismatch", ErrInvalidToken)
	}
	var c jwtClaims
	if err := decodePart(parts[1], &c); err != nil {
		return Claims{}, fmt.Errorf("%w: bad claims", ErrInvalidToken)
	}
	if !contains(p.Issuers, c.Issuer) {
		return Claims{}, fmt.Errorf("%w: wrong issuer", ErrInvalidToken)
	}
	if !audienceMatches(c.Audience, p.Audiences) {
		return Claims{}, fmt.Errorf("%w: wrong audience", ErrInvalidToken)
	}
	now := v.now()
	if c.Expires == 0 || now.After(time.Unix(c.Expires, 0).Add(clockSkew)) {
		return Claims{}, fmt.Errorf("%w: token expired", ErrInvalidToken)
	}
	if c.IssuedAt != 0 && time.Unix(c.IssuedAt, 0).After(now.Add(clockSkew)) {
		return Claims{}, fmt.Errorf("%w: token issued in the future", ErrInvalidToken)
	}
	if strings.TrimSpace(c.Subject) == "" {
		return Claims{}, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	hashed := sha256.Sum256([]byte(nonce))
	if !equal(c.Nonce, nonce) && !equal(c.Nonce, hex.EncodeToString(hashed[:])) {
		return Claims{}, fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}
	return Claims{Provider: p.Name, Subject: c.Subject}, nil
}

func equal(a, b string) bool { return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// audienceMatches accepts the JWT "aud" claim as either a string or an array of strings.
func audienceMatches(raw json.RawMessage, allowed []string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return contains(allowed, one)
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if contains(allowed, a) {
				return true
			}
		}
	}
	return false
}

func decodePart(part string, into any) error {
	b, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

// key returns the provider's public key for kid, refreshing the cached key set when it is stale
// or the key is unknown (providers rotate keys), but no more than once a minute.
func (v *Verifier) key(ctx context.Context, p Provider, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	ks := v.cache[p.Name]
	if ks != nil {
		if k, ok := ks.keys[kid]; ok && v.now().Sub(ks.fetchedAt) < jwksTTL {
			return k, nil
		}
		if v.now().Sub(ks.fetchedAt) < jwksRetryWait {
			return nil, fmt.Errorf("%w: unknown signing key", ErrInvalidToken)
		}
	}
	fresh, err := v.fetch(ctx, p)
	if err != nil {
		return nil, err
	}
	v.cache[p.Name] = fresh
	k, ok := fresh.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: unknown signing key", ErrInvalidToken)
	}
	return k, nil
}

func (v *Verifier) fetch(ctx context.Context, p Provider) (*keySet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("identity provider keys unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity provider keys unavailable: status %d", res.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxJWKSBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("identity provider keys unreadable: %w", err)
	}
	ks := &keySet{keys: map[string]*rsa.PublicKey{}, fetchedAt: v.now()}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(n) == 0 || len(e) == 0 {
			continue
		}
		ks.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	return ks, nil
}
