// Package commandcentre owns overview, timeline, medical history, goals and follow-ups (module 01).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package commandcentre

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

func (m *Module) Name() string { return "commandcentre" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/profiles/{profileID}/overview", m.overview)
	r.Get("/profiles/{profileID}/timeline", m.timeline)
	r.Get("/profiles/{profileID}/history", m.listHistory)
	r.Post("/profiles/{profileID}/history", m.createHistory)
	r.Patch("/history/{entryID}", m.updateHistory)
	r.Delete("/history/{entryID}", m.deleteHistory)
	r.Get("/profiles/{profileID}/goals", m.listGoals)
	r.Post("/profiles/{profileID}/goals", m.createGoal)
	r.Patch("/goals/{goalID}", m.updateGoal)
	r.Delete("/goals/{goalID}", m.deleteGoal)
	r.Get("/profiles/{profileID}/follow-ups", m.listFollowUps)
	r.Post("/profiles/{profileID}/follow-ups", m.createFollowUp)
	r.Patch("/follow-ups/{followUpID}", m.updateFollowUp)
	r.Delete("/follow-ups/{followUpID}", m.deleteFollowUp)
}
