// Package devices owns connected devices and their readings (module 05): the device registry, batched
// and idempotent sync from HealthKit or other sources, sync history, daily summaries, and the
// comparison between device readings and laboratory results.
//
// Device readings are stored apart from laboratory observations and are never presented as lab
// results. It may import internal/platform, internal/domain and internal/repo only.
package devices

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

func (m *Module) Name() string { return "devices" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/device-metrics", m.metricCatalog)
	r.Get("/profiles/{profileID}/devices", m.listDevices)
	r.Post("/profiles/{profileID}/devices", m.registerDevice)
	r.Patch("/profiles/{profileID}/devices/{deviceID}", m.updateDevice)
	r.Delete("/profiles/{profileID}/devices/{deviceID}", m.removeDevice)
	r.Post("/profiles/{profileID}/devices/{deviceID}/readings", m.syncReadings)
	r.Get("/profiles/{profileID}/devices/{deviceID}/syncs", m.syncHistory)
	r.Get("/profiles/{profileID}/device-readings", m.listReadings)
	r.Get("/profiles/{profileID}/device-readings/summary", m.readingSummary)
	r.Get("/profiles/{profileID}/device-readings/{metric}/daily", m.dailySeries)
	r.Get("/profiles/{profileID}/device-readings/{metric}/lab-comparison", m.labComparison)
}
