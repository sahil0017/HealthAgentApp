// Package biomarkers owns observations, biomarker history, trends and corrections (module 03).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package biomarkers

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

func (m *Module) Name() string { return "biomarkers" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/profiles/{profileID}/observations", m.listObservations)
	r.Post("/profiles/{profileID}/observations", m.createObservation)
	r.Get("/profiles/{profileID}/biomarkers", m.listBiomarkers)
	r.Get("/profiles/{profileID}/biomarkers/summary", m.biomarkerSummaries)
	r.Get("/profiles/{profileID}/biomarkers/compare", m.biomarkerCompare)
	r.Get("/profiles/{profileID}/biomarkers/coverage", m.biomarkerCoverage)
	r.Get("/profiles/{profileID}/biomarkers/{code}/history", m.biomarkerHistory)
	r.Get("/profiles/{profileID}/biomarkers/{code}/trend", m.biomarkerTrend)
	r.Post("/observations/{observationID}/verify", m.verifyObservation)
	r.Post("/observations/{observationID}/corrections", m.correctObservation)
}
