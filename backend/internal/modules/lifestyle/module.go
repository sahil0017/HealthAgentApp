// Package lifestyle owns the symptom journal, daily lifestyle logs and the correlations between them
// (module 09). Entries are self-reported and descriptive: correlations show association only and are
// never a diagnosis. It may import internal/platform, internal/domain and internal/repo only.
package lifestyle

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

func (m *Module) Name() string { return "lifestyle" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/lifestyle-categories", m.categories)
	r.Get("/profiles/{profileID}/symptoms", m.listSymptoms)
	r.Post("/profiles/{profileID}/symptoms", m.addSymptom)
	r.Get("/profiles/{profileID}/symptoms/summary", m.symptomSummary)
	r.Patch("/profiles/{profileID}/symptoms/{symptomID}", m.updateSymptom)
	r.Delete("/profiles/{profileID}/symptoms/{symptomID}", m.deleteSymptom)
	r.Get("/profiles/{profileID}/lifestyle-logs", m.listLogs)
	r.Post("/profiles/{profileID}/lifestyle-logs", m.addLog)
	r.Delete("/profiles/{profileID}/lifestyle-logs/{logID}", m.deleteLog)
	r.Get("/profiles/{profileID}/lifestyle/summary", m.lifestyleSummary)
	r.Get("/profiles/{profileID}/lifestyle/correlations", m.correlations)
}
