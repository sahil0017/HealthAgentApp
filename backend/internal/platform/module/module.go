// Package module defines the contract between the API composition root and each product module.
//
// A module owns one folder under internal/modules, its routes, its handlers and its tests. Modules
// depend only on internal/platform, internal/domain and internal/repo, never on one another; the
// architecture test in internal/archtest enforces that, so two people can work on different modules
// without touching the same files or imports.
package module

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Module registers its routes on the supplied routers.
type Module interface {
	// Name is the stable module id, matching its folder (for example "vault").
	Name() string
	// Public registers routes that need no session (sign-in, token-authenticated protocols).
	Public(r chi.Router)
	// Authed registers routes that run behind core.Authenticate.
	Authed(r chi.Router)
}

// Base gives modules no-op defaults so each implements only what it needs.
type Base struct{}

func (Base) Public(chi.Router) {}
func (Base) Authed(chi.Router) {}

// Mount registers every module: public routes first, then authenticated routes behind authenticate.
func Mount(r chi.Router, authenticate func(http.Handler) http.Handler, mods []Module) {
	for _, m := range mods {
		m.Public(r)
	}
	r.Group(func(r chi.Router) {
		r.Use(authenticate)
		for _, m := range mods {
			m.Authed(r)
		}
	})
}
