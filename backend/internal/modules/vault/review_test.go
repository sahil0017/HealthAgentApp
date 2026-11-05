package vault

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cortex/backend/internal/platform/testkit"
)

func TestApproveCreatesCatalogEntryForUnmatchedTest(t *testing.T) {
	c := testkit.Open(t)
	profile := testkit.SeedProfile(t, c, "owner-a")
	ctx := context.Background()
	if _, err := c.DB.Pool.Exec(ctx, `INSERT INTO consents(profile_id,purpose,granted) VALUES($1,'health_data',true) ON CONFLICT (profile_id,purpose) DO UPDATE SET granted=true,revoked_at=NULL`, profile); err != nil {
		t.Fatal(err)
	}
	h := testkit.Router("owner-a", New(c))
	approve := func(n int, unit string) int {
		var id string
		if err := c.DB.Pool.QueryRow(ctx, `INSERT INTO medical_documents(profile_id,owner_id,object_key,file_name,content_type,byte_size,status) VALUES($1,'owner-a',$2,'r.pdf','application/pdf',10,'review_required') RETURNING id::text`, profile, fmt.Sprintf("k-%s-%d", profile, n)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"biomarkers":[{"name":"Vitamin B12","value":310,"unit":%q}]}`, unit)
		return testkit.Call(h, "POST", "/profiles/"+profile+"/documents/"+id+"/approve", body).Code
	}
	if code := approve(1, "pg/mL"); code != http.StatusOK {
		t.Fatalf("approve = %d", code)
	}
	if code := approve(2, "pg/mL"); code != http.StatusOK {
		t.Fatalf("second approve = %d", code)
	}
	if n := testkit.Count(t, c, `SELECT count(*) FROM biomarker_catalog WHERE code='VITAMIN_B12' AND status='user_reported'`); n != 1 {
		t.Fatalf("catalog entries = %d, want 1", n)
	}
	if n := testkit.Count(t, c, `SELECT count(*) FROM observations WHERE profile_id=$1 AND biomarker_code='VITAMIN_B12'`, profile); n != 2 {
		t.Fatalf("linked observations = %d, want 2 (one trend)", n)
	}
	if code := approve(3, "ng/L"); code != http.StatusOK {
		t.Fatalf("different unit approve = %d", code)
	}
	if n := testkit.Count(t, c, `SELECT count(*) FROM observations WHERE profile_id=$1 AND biomarker_code IS NULL`, profile); n != 1 {
		t.Fatalf("a different unit must stay unlinked, got %d", n)
	}
}
