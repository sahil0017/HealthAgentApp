// Package testkit is the shared test harness: a migrated scratch database, an owner-injecting router
// and small helpers. Tests that need a database skip when TEST_DATABASE_URL is not set.
//
// It deliberately does not import internal/modules, so module packages can use it in their own tests.
package testkit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/platform/module"

	"cortex/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

// FakeRemover records deleted object keys and can fail on one of them.
type FakeRemover struct {
	Removed []string
	FailOn  string
}

func (f *FakeRemover) Remove(_ context.Context, key string) error {
	if key == f.FailOn {
		return errors.New("storage down")
	}
	f.Removed = append(f.Removed, key)
	return nil
}

// Open returns a Core on a migrated scratch database with a FakeRemover as its object remover.
func Open(t *testing.T) *core.Core {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	c := core.New(db, nil)
	c.Remover = &FakeRemover{}
	return c
}

// Router mounts the authenticated routes of mods with the request owner preset, bypassing sessions.
// An X-Test-Owner header overrides the owner so one test can act as several users.
func Router(owner string, mods ...module.Module) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			who := owner
			if v := req.Header.Get("X-Test-Owner"); v != "" {
				who = v
			}
			next.ServeHTTP(w, req.WithContext(httpx.WithOwner(req.Context(), who)))
		})
	})
	for _, m := range mods {
		m.Authed(r)
	}
	return r
}

func Call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	h.ServeHTTP(w, req)
	return w
}

func Count(t *testing.T, c *core.Core, query string, args ...any) int {
	t.Helper()
	var n int
	if err := c.DB.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// SeedProfile creates a profile with a document per object key, one observation with an embedding,
// and audit rows. It returns the profile id and removes it on cleanup.
func SeedProfile(t *testing.T, c *core.Core, ownerID string, objectKeys ...string) string {
	t.Helper()
	ctx := context.Background()
	pool := c.DB.Pool
	var profileID string
	if err := pool.QueryRow(ctx, `INSERT INTO profiles(owner_id,display_name) VALUES($1,'Test') RETURNING id::text`, ownerID).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM profiles WHERE id=$1`, profileID) })
	if _, err := pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'ai_processing',true)`, profileID); err != nil {
		t.Fatal(err)
	}
	for i, key := range objectKeys {
		var docID string
		if err := pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1,$2,$3,'r.pdf','application/pdf',10,'approved') RETURNING id::text`, profileID, ownerID, key).Scan(&docID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO outbox_events(event_type,aggregate_type,aggregate_id) VALUES('document.uploaded','medical_document',$1)`, docID); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			var obsID string
			if err := pool.QueryRow(ctx, `INSERT INTO observations(profile_id,document_id,owner_id,name,value,unit) VALUES($1,$2,$3,'Glucose',95,'mg/dL') RETURNING id::text`, profileID, docID, ownerID).Scan(&obsID); err != nil {
				t.Fatal(err)
			}
			vector := "[" + strings.TrimSuffix(strings.Repeat("0.1,", 384), ",") + "]"
			if _, err := pool.Exec(ctx, `INSERT INTO record_chunks(owner_id,profile_id,observation_id,observation_revision,content,embedding,embedding_model) VALUES($1,$2,$3,1,'Glucose 95',$4::vector,'test')`, ownerID, profileID, obsID, vector); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type) VALUES($1,$2,'document.upload_completed','medical_document'),($1,$2,'mcp.tool_called','profile')`, ownerID, profileID); err != nil {
		t.Fatal(err)
	}
	return profileID
}
