// Package agent is the assistant layer (module 19): a daily briefing, a doctor-visit summary and a
// propose-then-approve action queue, all built from the user's own records. The agent never writes to
// the record by itself: it proposes, and an action only takes effect when the user approves it. It makes
// no diagnoses and invents no clinical content; every line is a fact from the record. It may import
// internal/platform, internal/domain and internal/repo only.
package agent

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

func (m *Module) Name() string { return "agent" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/profiles/{profileID}/agent/permissions", m.listPermissions)
	r.Put("/profiles/{profileID}/agent/permissions/{permission}", m.setPermission)
	r.Get("/profiles/{profileID}/agent/briefing", m.briefing)
	r.Get("/profiles/{profileID}/agent/briefing/summary", m.briefingSummary)
	r.Get("/profiles/{profileID}/agent/visit-prep", m.visitPrep)
	r.Get("/profiles/{profileID}/agent/suggestions", m.suggestions)
	r.Get("/profiles/{profileID}/agent/wellbeing", m.wellbeingPlan)
	r.Get("/profiles/{profileID}/agent/insights/uploads", m.uploadStats)
	r.Get("/profiles/{profileID}/agent/insights/document/{documentID}", m.documentInsight)
	r.Get("/profiles/{profileID}/agent/insights/biomarker/{code}", m.biomarkerInsight)
	r.Get("/profiles/{profileID}/agent/insights/{kind}", m.insights)
	r.Post("/profiles/{profileID}/agent/chat", m.chat)
	r.Post("/profiles/{profileID}/agent/plan", m.plan)
	r.Get("/profiles/{profileID}/agent/actions", m.listActions)
	r.Post("/profiles/{profileID}/agent/actions/{actionID}/approve", m.approve)
	r.Post("/profiles/{profileID}/agent/actions/{actionID}/reject", m.reject)
	r.Get("/profiles/{profileID}/agent/activity", m.activity)
}
