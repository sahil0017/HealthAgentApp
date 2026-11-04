package medications

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func setup(t *testing.T) http.Handler {
	t.Helper()
	rx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/spellingsuggestions.json"):
			_, _ = w.Write([]byte(`{"suggestionGroup":{"suggestionList":{"suggestion":["metformin"]}}}`))
		case r.URL.Query().Get("name") == "metformin":
			_, _ = w.Write([]byte(`{"drugGroup":{"conceptGroup":[{"tty":"SCD","conceptProperties":[{"rxcui":"860975","name":"metformin 500 MG Oral Tablet"}]}]}}`))
		default:
			_, _ = w.Write([]byte(`{"drugGroup":{}}`))
		}
	}))
	fda := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("search"), "nothing") {
			http.Error(w, `{"error":{"code":"NOT_FOUND"}}`, 404)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"indications_and_usage":["Type 2   diabetes."],"boxed_warning":["Lactic acidosis."],"openfda":{"brand_name":["Glucophage"],"generic_name":["METFORMIN"]}}]}`))
	}))
	t.Cleanup(rx.Close)
	t.Cleanup(fda.Close)
	m := &Module{HTTP: http.DefaultClient, RxNavURL: rx.URL, OpenFDA: fda.URL, cache: map[string]cached{}}
	r := chi.NewRouter()
	m.Authed(r)
	return r
}

func get(h http.Handler, p string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
	return rec
}

func TestSearchFindsMedicinesAndSuggestsSpelling(t *testing.T) {
	h := setup(t)
	if b := get(h, "/medications/search?q=metformin").Body.String(); !strings.Contains(b, "860975") || !strings.Contains(b, "Generic product") || !strings.Contains(b, "Indian brands") {
		t.Fatal(b)
	}
	b := get(h, "/medications/search?q=metfromin").Body.String()
	if !strings.Contains(b, `"did_you_mean":"metformin"`) || !strings.Contains(b, "860975") {
		t.Fatal(b)
	}
}

func TestLabelQuotesFDATextAndHandlesUnknown(t *testing.T) {
	h := setup(t)
	b := get(h, "/medications/label?name=metformin").Body.String()
	if !strings.Contains(b, "Type 2 diabetes.") || !strings.Contains(b, "Lactic acidosis.") || !strings.Contains(b, "Glucophage") {
		t.Fatal(b)
	}
	if c := get(h, "/medications/label?name=nothing").Code; c != 404 {
		t.Fatal(c)
	}
}

func TestQueryIsValidated(t *testing.T) {
	h := setup(t)
	for _, p := range []string{"/medications/search", "/medications/search?q=a", "/medications/label?name=" + strings.Repeat("x", 90)} {
		if c := get(h, p).Code; c != 400 {
			t.Fatalf("%s: %d", p, c)
		}
	}
}
