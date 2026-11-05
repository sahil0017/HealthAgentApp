// Package prescriptions reads a photographed doctor's prescription through the AI service (only with the
// person's explicit consent), lets the person confirm every medicine, and turns each medicine's timing into a
// general daily schedule. It never verifies drugs or gives dosing advice. It may import internal/platform,
// internal/domain and internal/repo only.
package prescriptions

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

func (m *Module) Name() string { return "prescriptions" }

func (m *Module) Authed(r chi.Router) {
	r.Post("/profiles/{profileID}/prescriptions/read", m.read)
	r.Get("/profiles/{profileID}/prescriptions", m.list)
	r.Get("/profiles/{profileID}/prescriptions/from-document/{documentID}", m.fromDocument)
	r.Post("/profiles/{profileID}/prescriptions", m.save)
	r.Get("/profiles/{profileID}/prescriptions/schedule", m.dailySchedule)
	r.Post("/profiles/{profileID}/prescriptions/doses", m.logDose)
	r.Delete("/profiles/{profileID}/prescriptions/{rxID}", m.remove)
}
