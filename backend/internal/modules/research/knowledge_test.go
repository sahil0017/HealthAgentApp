package research

import (
	"cortex/backend/internal/platform/httpx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type httpRequest struct{ *http.Request }

func TestIsAdmin(t *testing.T) {
	a := New(nil)
	mk := func(id string) *httpRequest {
		r := httptest.NewRequest("GET", "/", nil)
		return &httpRequest{r.WithContext(httpx.WithOwner(r.Context(), id))}
	}
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "admin-1, admin-2")
	if !a.isAdmin(mk("admin-2").Request) {
		t.Error("allowlisted owner should be admin")
	}
	if a.isAdmin(mk("someone").Request) {
		t.Error("unlisted owner must not be admin")
	}
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "")
	if a.isAdmin(mk("").Request) {
		t.Error("empty owner must not match an empty allowlist")
	}
}

func TestRequireAdminRejectsWithForbidden(t *testing.T) {
	t.Setenv("CORTEX_ADMIN_OWNER_IDS", "")
	w := httptest.NewRecorder()
	if New(nil).requireAdmin(w, httptest.NewRequest("GET", "/", nil)) || w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestChunkAbstractSplitsOnSentencesAndKeepsTitle(t *testing.T) {
	sentence := strings.Repeat("word ", 40) + "end. "
	chunks := chunkAbstract("Title", strings.Repeat(sentence, 12))
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if !strings.HasPrefix(c, "Title\n") || len(c) > 1200 {
			t.Fatalf("bad chunk (%d chars): %.40q", len(c), c)
		}
	}
	if got := chunkAbstract("T", "   "); len(got) != 0 {
		t.Fatalf("blank abstract should yield no chunks, got %v", got)
	}
}
