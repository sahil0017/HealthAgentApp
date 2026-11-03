// Package identity owns sessions, the signed-in user and profiles.
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package identity

import (
	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

// Module implements module.Module.
type Module struct {
	module.Base
	*core.Core
}

func New(c *core.Core) *Module { return &Module{Core: c} }

func (m *Module) Name() string { return "identity" }

func (m *Module) Public(r chi.Router) {
	r.Post("/auth/session", m.createSession)
}

func (m *Module) Authed(r chi.Router) {
	r.Post("/auth/logout", m.logout)
	r.Get("/users/me", m.currentUser)
	r.Get("/profiles", m.listProfiles)
	r.Post("/profiles", m.createProfile)
	r.Get("/profiles/{profileID}", m.getProfile)
	r.Patch("/profiles/{profileID}", m.updateProfile)
}
