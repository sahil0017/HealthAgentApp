// Package identity owns sessions, the signed-in user and profiles.
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package identity

import (
	"os"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

// Module implements module.Module.
type Module struct {
	module.Base
	*core.Core
	OIDC *Verifier
}

// New builds the module with identity providers taken from the environment.
func New(c *core.Core) *Module {
	return &Module{Core: c, OIDC: NewVerifier(LoadProviders(os.Getenv))}
}

func (m *Module) Name() string { return "identity" }

func (m *Module) Public(r chi.Router) {
	r.Get("/auth/config", m.authConfig)
	r.Post("/auth/session", m.createSession)
}

func (m *Module) Authed(r chi.Router) {
	r.Post("/auth/logout", m.logout)
	r.Post("/auth/refresh", m.refresh)
	r.Post("/auth/logout-all", m.logoutEverywhere)
	r.Get("/auth/sessions", m.listSessions)
	r.Delete("/auth/sessions/{sessionID}", m.revokeSession)
	r.Get("/users/me", m.currentUser)
	r.Get("/profiles", m.listProfiles)
	r.Post("/profiles", m.createProfile)
	r.Get("/profiles/{profileID}", m.getProfile)
	r.Patch("/profiles/{profileID}", m.updateProfile)
}
