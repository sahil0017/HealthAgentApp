// Package governance owns consents, access history, export and deletion (module 12).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package governance

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

func (m *Module) Name() string { return "governance" }

func (m *Module) Authed(r chi.Router) {
	r.Delete("/profiles/{profileID}", m.deleteProfile)
	r.Get("/profiles/{profileID}/access-history", m.accessHistory)
	r.Get("/profiles/{profileID}/export", m.exportProfile)
	r.Get("/profiles/{profileID}/consents", m.getConsents)
	r.Post("/profiles/{profileID}/consents", m.createConsent)
	r.Put("/profiles/{profileID}/consents/{purpose}", m.setConsent)
	r.Get("/profiles/{profileID}/retention", m.getRetention)
	r.Put("/profiles/{profileID}/retention", m.setRetention)
	r.Post("/profiles/{profileID}/retention/apply", m.applyRetention)
	r.Get("/profiles/{profileID}/shares", m.listShares)
	r.Post("/profiles/{profileID}/shares", m.createShare)
	r.Delete("/profiles/{profileID}/shares/{shareID}", m.revokeShare)
	r.Post("/shares/accept", m.acceptShare)
	r.Get("/shared-with-me", m.sharedWithMe)
	r.Get("/shared-with-me/{shareID}", m.viewShared)
	r.Delete("/shared-with-me/{shareID}", m.leaveShare)
	r.Get("/consents/{consentID}", m.getConsent)
	r.Delete("/consents/{consentID}", m.withdrawConsent)
}
