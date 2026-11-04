// Package nearby serves location-aware public-health information (module 20): outbreak alerts and
// hospitals for the user's coarse area. It is stateless and stores nothing about where a user is:
// coordinates are rounded on arrival and are never logged or persisted. Every item names its source,
// and a category with no verified source reports that instead of inventing data. It may import
// internal/platform only.
package nearby

import (
	"net/http"
	"sync"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

const (
	whoURL      = "https://www.who.int/api/news/diseaseoutbreaknews"
	overpassURL = "https://overpass-api.de/api/interpreter"
	userAgent   = "SehatApp/1.0 (personal health app)"
	cacheTTL    = 10 * time.Minute
)

// Module implements module.Module.
type Module struct {
	module.Base
	*core.Core
	HTTP        *http.Client
	WHOURL      string
	OverpassURL string

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	data any
}

func New(c *core.Core) *Module {
	return &Module{
		Core:        c,
		HTTP:        &http.Client{Timeout: 25 * time.Second},
		WHOURL:      whoURL,
		OverpassURL: overpassURL,
		cache:       map[string]cached{},
	}
}

func (m *Module) Name() string { return "nearby" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/nearby/sources", m.sources)
	r.Get("/nearby/alerts", m.alerts)
	r.Get("/nearby/hospitals", m.hospitals)
	r.Get("/nearby/places", m.places)
}

func (m *Module) fromCache(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cache[key]
	if !ok || time.Since(c.at) > cacheTTL {
		return nil, false
	}
	return c.data, true
}

func (m *Module) toCache(key string, v any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) > 500 {
		m.cache = map[string]cached{}
	}
	m.cache[key] = cached{at: time.Now(), data: v}
}
