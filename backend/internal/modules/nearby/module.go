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
	whoURL          = "https://www.who.int/api/news/diseaseoutbreaknews"
	overpassURL     = "https://overpass-api.de/api/interpreter"
	userAgent       = "SehatApp/1.0 (personal health app)"
	cacheTTL        = 10 * time.Minute
	overpassBackoff = 3 * time.Minute
	// staleTTL is how long an old answer may still be served when every map server is busy.
	staleTTL = 24 * time.Hour
)

// overpassMirrors are tried after the primary server, because the public servers shed load with 502s.
var overpassMirrors = []string{
	"https://overpass.kumi.systems/api/interpreter",
	"https://overpass.private.coffee/api/interpreter",
}

// Module implements module.Module.
type Module struct {
	module.Base
	*core.Core
	HTTP        *http.Client
	WHOURL      string
	OverpassURL string
	Mirrors     []string
	// SearchURL is a Nominatim endpoint used when no Overpass server answers; empty disables it.
	SearchURL string
	// NewsURL and LocalNewsURL are RSS endpoints for worldwide and city-level health news.
	NewsURL, LocalNewsURL string

	overpassDownUntil time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	data any
}

func New(c *core.Core) *Module {
	return &Module{
		Core:         c,
		HTTP:         &http.Client{Timeout: 25 * time.Second},
		WHOURL:       whoURL,
		OverpassURL:  overpassURL,
		Mirrors:      overpassMirrors,
		SearchURL:    "https://nominatim.openstreetmap.org/search",
		NewsURL:      whoNewsURL,
		LocalNewsURL: localNewsFn,
		cache:        map[string]cached{},
	}
}

func (m *Module) Name() string { return "nearby" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/nearby/sources", m.sources)
	r.Get("/nearby/alerts", m.alerts)
	r.Get("/nearby/hospitals", m.hospitals)
	r.Get("/nearby/news", m.news)
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

// stale returns an answer of any age up to staleTTL, for when the live servers are unavailable.
func (m *Module) stale(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cache[key]
	if !ok || time.Since(c.at) > staleTTL {
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
