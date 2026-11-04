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
	"DELETE /auth/sessions/{sessionID}",
	"DELETE /consents/{consentID}",
	"DELETE /documents/{documentID}",
	"DELETE /follow-ups/{followUpID}",
	"DELETE /goals/{goalID}",
	"DELETE /history/{entryID}",
	"DELETE /profiles/{profileID}",
	"DELETE /profiles/{profileID}/devices/{deviceID}",
	"DELETE /profiles/{profileID}/investigations/{investigationID}",
	"DELETE /profiles/{profileID}/investigations/{investigationID}/items/{itemID}",
	"DELETE /profiles/{profileID}/lifestyle-logs/{logID}",
	"DELETE /profiles/{profileID}/questions/{questionID}",
	"DELETE /profiles/{profileID}/shares/{shareID}",
	"DELETE /profiles/{profileID}/symptoms/{symptomID}",
	"DELETE /shared-with-me/{shareID}",
	"GET /auth/config",
	"GET /auth/sessions",
	"GET /consents/{consentID}",
	"GET /device-metrics",
	"GET /documents/{documentID}",
	"GET /documents/{documentID}/download",
	"GET /documents/{documentID}/extraction/pages",
	"GET /documents/{documentID}/history",
	"GET /documents/{documentID}/versions",
	"GET /knowledge/entries",
	"GET /lifestyle-categories",
	"GET /nearby/alerts",
	"GET /nearby/hospitals",
	"GET /nearby/sources",
	"GET /profiles",
	"GET /profiles/{profileID}",
	"GET /profiles/{profileID}/access-history",
	"GET /profiles/{profileID}/agent/actions",
	"GET /profiles/{profileID}/agent/activity",
	"GET /profiles/{profileID}/agent/briefing",
	"GET /profiles/{profileID}/agent/permissions",
	"GET /profiles/{profileID}/agent/visit-prep",
	"GET /profiles/{profileID}/biomarkers",
	"GET /profiles/{profileID}/biomarkers/compare",
	"GET /profiles/{profileID}/biomarkers/coverage",
	"GET /profiles/{profileID}/biomarkers/summary",
	"GET /profiles/{profileID}/biomarkers/{code}/history",
	"GET /profiles/{profileID}/biomarkers/{code}/trend",
	"GET /profiles/{profileID}/consents",
	"GET /profiles/{profileID}/device-readings",
	"GET /profiles/{profileID}/device-readings/summary",
	"GET /profiles/{profileID}/device-readings/{metric}/daily",
	"GET /profiles/{profileID}/device-readings/{metric}/lab-comparison",
	"GET /profiles/{profileID}/devices",
	"GET /profiles/{profileID}/devices/{deviceID}/syncs",
	"GET /profiles/{profileID}/documents",
	"GET /profiles/{profileID}/documents/search",
	"GET /profiles/{profileID}/documents/{documentID}",
	"GET /profiles/{profileID}/documents/{documentID}/file",
	"GET /profiles/{profileID}/export",
	"GET /profiles/{profileID}/follow-ups",
	"GET /profiles/{profileID}/goals",
	"GET /profiles/{profileID}/history",
	"GET /profiles/{profileID}/investigations",
	"GET /profiles/{profileID}/investigations/{investigationID}",
	"GET /profiles/{profileID}/lifestyle-logs",
	"GET /profiles/{profileID}/lifestyle/correlations",
	"GET /profiles/{profileID}/lifestyle/summary",
	"GET /profiles/{profileID}/observations",
	"GET /profiles/{profileID}/overview",
	"GET /profiles/{profileID}/questions",
	"GET /profiles/{profileID}/questions/{questionID}",
	"GET /profiles/{profileID}/retention",
	"GET /profiles/{profileID}/shares",
	"GET /profiles/{profileID}/symptoms",
	"GET /profiles/{profileID}/symptoms/summary",
	"GET /profiles/{profileID}/timeline",
	"GET /shared-with-me",
	"GET /shared-with-me/{shareID}",
	"GET /users/me",
	"PATCH /documents/{documentID}",
	"PATCH /follow-ups/{followUpID}",
	"PATCH /goals/{goalID}",
	"PATCH /history/{entryID}",
	"PATCH /profiles/{profileID}",
	"PATCH /profiles/{profileID}/devices/{deviceID}",
	"PATCH /profiles/{profileID}/investigations/{investigationID}",
	"PATCH /profiles/{profileID}/symptoms/{symptomID}",
	"POST /auth/logout",
	"POST /auth/logout-all",
	"POST /auth/refresh",
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
	"POST /profiles/{profileID}/agent/actions/{actionID}/approve",
	"POST /profiles/{profileID}/agent/actions/{actionID}/reject",
	"POST /profiles/{profileID}/agent/plan",
	"POST /profiles/{profileID}/consents",
	"POST /profiles/{profileID}/devices",
	"POST /profiles/{profileID}/devices/{deviceID}/readings",
	"POST /profiles/{profileID}/documents",
	"POST /profiles/{profileID}/documents/upload-intents",
	"POST /profiles/{profileID}/documents/{documentID}/approve",
	"POST /profiles/{profileID}/documents/{documentID}/retry",
	"POST /profiles/{profileID}/follow-ups",
	"POST /profiles/{profileID}/goals",
	"POST /profiles/{profileID}/history",
	"POST /profiles/{profileID}/investigations",
	"POST /profiles/{profileID}/investigations/{investigationID}/items",
	"POST /profiles/{profileID}/investigations/{investigationID}/refresh",
	"POST /profiles/{profileID}/lifestyle-logs",
	"POST /profiles/{profileID}/mcp-tokens",
	"POST /profiles/{profileID}/observations",
	"POST /profiles/{profileID}/questions",
	"POST /profiles/{profileID}/retention/apply",
	"POST /profiles/{profileID}/shares",
	"POST /profiles/{profileID}/symptoms",
	"POST /shares/accept",
	"PUT /profiles/{profileID}/agent/permissions/{permission}",
	"PUT /profiles/{profileID}/consents/{purpose}",
	"PUT /profiles/{profileID}/questions/{questionID}/feedback",
	"PUT /profiles/{profileID}/retention",
}

func TestRouteTableMatchesTheAPIContract(t *testing.T) {
	router := Router(core.New(nil, nil)).(chi.Router)
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
