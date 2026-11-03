package api

import (
	"net/http"
	"sort"
	"testing"

	"cortex/backend/internal/platform/core"

	"github.com/go-chi/chi/v5"
)

// expectedRoutes is the public API surface under /v1. Changing it is a deliberate API change:
// update backend/openapi.yaml in the same commit.
var expectedRoutes = []string{
	"DELETE /consents/{consentID}",
	"DELETE /documents/{documentID}",
	"DELETE /follow-ups/{followUpID}",
	"DELETE /goals/{goalID}",
	"DELETE /history/{entryID}",
	"DELETE /profiles/{profileID}",
	"GET /consents/{consentID}",
	"GET /documents/{documentID}",
	"GET /documents/{documentID}/download",
	"GET /documents/{documentID}/extraction/pages",
	"GET /documents/{documentID}/history",
	"GET /documents/{documentID}/versions",
	"GET /knowledge/entries",
	"GET /profiles",
	"GET /profiles/{profileID}",
	"GET /profiles/{profileID}/access-history",
	"GET /profiles/{profileID}/biomarkers",
	"GET /profiles/{profileID}/biomarkers/compare",
	"GET /profiles/{profileID}/biomarkers/coverage",
	"GET /profiles/{profileID}/biomarkers/summary",
	"GET /profiles/{profileID}/biomarkers/{code}/history",
	"GET /profiles/{profileID}/biomarkers/{code}/trend",
	"GET /profiles/{profileID}/consents",
	"GET /profiles/{profileID}/documents",
	"GET /profiles/{profileID}/documents/search",
	"GET /profiles/{profileID}/documents/{documentID}",
	"GET /profiles/{profileID}/documents/{documentID}/file",
	"GET /profiles/{profileID}/export",
	"GET /profiles/{profileID}/follow-ups",
	"GET /profiles/{profileID}/goals",
	"GET /profiles/{profileID}/history",
	"GET /profiles/{profileID}/observations",
	"GET /profiles/{profileID}/overview",
	"GET /profiles/{profileID}/timeline",
	"GET /users/me",
	"PATCH /documents/{documentID}",
	"PATCH /follow-ups/{followUpID}",
	"PATCH /goals/{goalID}",
	"PATCH /history/{entryID}",
	"PATCH /profiles/{profileID}",
	"POST /auth/logout",
	"POST /auth/session",
	"POST /documents/{documentID}/complete",
	"POST /knowledge/entries/bulk-approve",
	"POST /knowledge/entries/{entryID}/approve",
	"POST /knowledge/entries/{entryID}/reject",
	"POST /knowledge/research",
	"POST /mcp",
	"POST /observations/{observationID}/corrections",
	"POST /observations/{observationID}/verify",
	"POST /profiles",
	"POST /profiles/{profileID}/consents",
	"POST /profiles/{profileID}/documents",
	"POST /profiles/{profileID}/documents/upload-intents",
	"POST /profiles/{profileID}/documents/{documentID}/approve",
	"POST /profiles/{profileID}/documents/{documentID}/retry",
	"POST /profiles/{profileID}/follow-ups",
	"POST /profiles/{profileID}/goals",
	"POST /profiles/{profileID}/history",
	"POST /profiles/{profileID}/mcp-tokens",
	"POST /profiles/{profileID}/observations",
	"POST /profiles/{profileID}/questions",
	"PUT /profiles/{profileID}/consents/{purpose}",
}

func TestRouteTableMatchesTheAPIContract(t *testing.T) {
	router := Router(core.New(nil, nil, "")).(chi.Router)
	var got []string
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if len(route) > 3 && route[:3] == "/v1" {
			got = append(got, method+" "+route[3:])
		}
		return nil
	})
	sort.Strings(got)
	if len(got) != len(expectedRoutes) {
		t.Fatalf("route count %d, want %d", len(got), len(expectedRoutes))
	}
	for i := range got {
		if got[i] != expectedRoutes[i] {
			t.Fatalf("route %d is %q, want %q", i, got[i], expectedRoutes[i])
		}
	}
}
