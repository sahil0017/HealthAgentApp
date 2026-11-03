// Package api is the composition root: it builds the shared core, mounts every module, and
// adds the platform endpoints. Feature code lives in internal/modules, not here.
package api

import (
	"context"
	"net/http"
	"time"

	"cortex/backend/internal/modules"
	"cortex/backend/internal/objects"
	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/platform/module"
	"cortex/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

func New(db *store.Store, objectStore *objects.Store) http.Handler {
	return Router(core.New(db, objectStore))
}

// Router mounts all modules under both /api/v1 and /v1.
func Router(c *core.Core) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestContext)
	r.Use(httpx.AccessLog)
	r.Use(httpx.RecoverJSON)
	r.Get("/healthz", healthz)
	r.Get("/readyz", ready(c))
	mods := modules.All(c)
	for _, prefix := range []string{"/api/v1", "/v1"} {
		r.Route(prefix, func(r chi.Router) { module.Mount(r, c.Authenticate, mods) })
	}
	return r
}

func healthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "cortex-api"})
}

func ready(c *core.Core) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := c.DB.Pool.Ping(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "dependency": "database"})
			return
		}
		if err := c.Objects.Check(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "dependency": "object_storage"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "cortex-api"})
	}
}
