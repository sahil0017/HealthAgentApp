package httpx

import (
	"context"
	"net/http"
)

type ownerKey struct{}
type sessionHashKey struct{}
type requestIDKey struct{}

// WithSession attaches the authenticated owner and the hash of the session token to a request context.
func WithSession(ctx context.Context, ownerID, sessionHash string) context.Context {
	ctx = context.WithValue(ctx, ownerKey{}, ownerID)
	return context.WithValue(ctx, sessionHashKey{}, sessionHash)
}

// WithOwner attaches only the owner; tests and internal callers use it.
func WithOwner(ctx context.Context, ownerID string) context.Context {
	return context.WithValue(ctx, ownerKey{}, ownerID)
}

// Owner is the authenticated user id, or "" when the request is unauthenticated.
func Owner(r *http.Request) string {
	v, _ := r.Context().Value(ownerKey{}).(string)
	return v
}

// SessionHash is the hash of the bearer token that authenticated the request.
func SessionHash(r *http.Request) string {
	v, _ := r.Context().Value(sessionHashKey{}).(string)
	return v
}

// RequestID is the id assigned by the RequestContext middleware.
func RequestID(r *http.Request) string {
	v, _ := r.Context().Value(requestIDKey{}).(string)
	return v
}
