package api

import (
	"testing"

	"cortex/backend/internal/modules"
	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/testkit"

	"net/http"
)

type fakeRemover = testkit.FakeRemover

var (
	call        = testkit.Call
	seedProfile = testkit.SeedProfile
)

func count(t *testing.T, c *core.Core, query string, args ...any) int {
	t.Helper()
	return testkit.Count(t, c, query, args...)
}

// newAPI is the integration harness: every module mounted on one scratch database, acting as ownerID.
func newAPI(t *testing.T, ownerID string, remover core.ObjectRemover) (*core.Core, http.Handler) {
	t.Helper()
	c := testkit.Open(t)
	c.Remover = remover
	return c, testkit.Router(ownerID, modules.All(c)...)
}
