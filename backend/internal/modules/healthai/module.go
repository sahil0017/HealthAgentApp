// Package healthai owns cited question answering, record retrieval and the MCP endpoint (module 07).
//
// It may import internal/platform, internal/domain and internal/repo only, never another module.
package healthai

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

func (m *Module) Name() string { return "healthai" }

func (m *Module) Public(r chi.Router) {
	r.Post("/mcp", m.mcpEndpoint)
}

func (m *Module) Authed(r chi.Router) {
	r.Post("/profiles/{profileID}/questions", m.askQuestion)
	r.Post("/profiles/{profileID}/mcp-tokens", m.createMCPToken)
}
