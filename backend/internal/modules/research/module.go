// Package research owns the reviewed knowledge base and research queue (module 08).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package research

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

func (m *Module) Name() string { return "research" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/profiles/{profileID}/investigations", m.listInvestigations)
	r.Post("/profiles/{profileID}/investigations", m.createInvestigation)
	r.Get("/profiles/{profileID}/investigations/{investigationID}", m.getInvestigation)
	r.Patch("/profiles/{profileID}/investigations/{investigationID}", m.updateInvestigation)
	r.Delete("/profiles/{profileID}/investigations/{investigationID}", m.deleteInvestigation)
	r.Post("/profiles/{profileID}/investigations/{investigationID}/items", m.addInvestigationItem)
	r.Delete("/profiles/{profileID}/investigations/{investigationID}/items/{itemID}", m.deleteInvestigationItem)
	r.Post("/profiles/{profileID}/investigations/{investigationID}/refresh", m.refreshInvestigation)
	r.Get("/knowledge/entries", m.listKnowledge)
	r.Post("/knowledge/research", m.requestResearch)
	r.Post("/knowledge/entries/bulk-approve", m.bulkApproveKnowledge)
	r.Post("/knowledge/entries/{entryID}/approve", m.approveKnowledge)
	r.Post("/knowledge/entries/{entryID}/reject", m.rejectKnowledge)
}
