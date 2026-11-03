// Package modules is the single place that lists every product module. Adding a module means adding
// one line here; no other file in the API needs to change.
package modules

import (
	"cortex/backend/internal/modules/biomarkers"
	"cortex/backend/internal/modules/commandcentre"
	"cortex/backend/internal/modules/governance"
	"cortex/backend/internal/modules/healthai"
	"cortex/backend/internal/modules/identity"
	"cortex/backend/internal/modules/research"
	"cortex/backend/internal/modules/vault"
	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/module"
)

// All builds every module on the shared core.
func All(c *core.Core) []module.Module {
	return []module.Module{
		identity.New(c),
		governance.New(c),
		commandcentre.New(c),
		biomarkers.New(c),
		vault.New(c),
		research.New(c),
		healthai.New(c),
	}
}
