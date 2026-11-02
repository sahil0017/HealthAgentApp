package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsOnlyTheTermAndDecodesProposals(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/research/biomarker" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"proposal":{"key":"GLUCOSE","payload":{"code":"GLUCOSE"},"evidence":[],"agreement":2}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	p, err := c.Biomarker(context.Background(), "Glucose", "")
	if err != nil || p == nil || p.Key != "GLUCOSE" || p.Agreement != 2 {
		t.Fatalf("unexpected result %+v %v", p, err)
	}
	if len(got) != 2 || got["term"] != "Glucose" || got["medlineplus_url"] != nil {
		t.Fatalf("request leaked or lost fields: %v", got)
	}
}

func TestClientReportsServiceErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Literature(context.Background(), "x"); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
	if _, _, err := c.Discover(context.Background(), 1, 0); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}

func TestUnitValidEscapesQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("unit") != "mg/dL" {
			t.Errorf("unit not passed intact: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"valid_ucum":true}`))
	}))
	defer srv.Close()
	ok, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).UnitValid(context.Background(), "mg/dL")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}
}
