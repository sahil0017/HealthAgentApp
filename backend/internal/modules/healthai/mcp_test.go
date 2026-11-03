package healthai

import (
	"cortex/backend/internal/platform/embedding"
	"testing"
	"time"
)

func TestMCPTokenRoundTripAndRejection(t *testing.T) {
	now := time.Now()
	claims := mcpClaims{Owner: "u1", Profile: "p1", Scope: mcpScopeRecords, Expires: now.Add(time.Minute).Unix()}
	token := signMCPToken(claims)
	got, err := verifyMCPToken(token, now)
	if err != nil || got != claims {
		t.Fatalf("valid token rejected: %v %+v", err, got)
	}
	cases := map[string]string{
		"tampered":    token[:len(token)-2] + "AA",
		"no-dot":      "abc",
		"expired":     signMCPToken(mcpClaims{Owner: "u1", Profile: "p1", Scope: mcpScopeRecords, Expires: now.Add(-time.Second).Unix()}),
		"wrong-scope": signMCPToken(mcpClaims{Owner: "u1", Profile: "p1", Scope: "records:write", Expires: now.Add(time.Minute).Unix()}),
		"no-profile":  signMCPToken(mcpClaims{Owner: "u1", Scope: mcpScopeRecords, Expires: now.Add(time.Minute).Unix()}),
	}
	for name, bad := range cases {
		if _, err := verifyMCPToken(bad, now); err == nil {
			t.Errorf("%s token was accepted", name)
		}
	}
}

func TestVectorLiteralAndObservationText(t *testing.T) {
	if got := embedding.VectorLiteral([]float32{0.5, -1, 0}); got != "[0.5,-1,0]" {
		t.Fatalf("got %s", got)
	}
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if got := observationText("HbA1c", 6.1, "%", &day, "4-5.6"); got != "HbA1c: 6.1 % on 2026-03-01 (reference 4-5.6)" {
		t.Fatalf("got %s", got)
	}
	if got := observationText("LDL", 130, "mg/dL", nil, ""); got != "LDL: 130 mg/dL" {
		t.Fatalf("got %s", got)
	}
}
