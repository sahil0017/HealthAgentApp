package environment

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func setup(t *testing.T, aqi string, fail bool) http.Handler {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "x", 500)
			return
		}
		if strings.Contains(r.URL.Path, "air-quality") {
			_, _ = w.Write([]byte(`{"current":{"us_aqi":` + aqi + `,"pm2_5":40},"current_units":{"pm2_5":"μg/m³"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":31},"current_units":{"temperature_2m":"°C"}}`))
	}))
	t.Cleanup(up.Close)
	m := &Module{HTTP: http.DefaultClient, WeatherURL: up.URL + "/forecast", AirURL: up.URL + "/air-quality", cache: map[string]cached{}}
	r := chi.NewRouter()
	m.Authed(r)
	return r
}

func get(h http.Handler, p string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
	return rec
}

func TestNowReportsAirAndWeatherWithCategory(t *testing.T) {
	rec := get(setup(t, "160", false), "/environment/now?lat=28.61&lng=77.2")
	b := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(b, "Unhealthy") || !strings.Contains(b, "temperature_2m") || !strings.Contains(b, "Open-Meteo") {
		t.Fatalf("%d %s", rec.Code, b)
	}
}

func TestNowRejectsBadArea(t *testing.T) {
	if rec := get(setup(t, "10", false), "/environment/now?lat=abc&lng=1"); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := get(setup(t, "10", false), "/environment/now?lat=99&lng=1"); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
}

func TestUpstreamFailureIsReported(t *testing.T) {
	if rec := get(setup(t, "10", true), "/environment/now?lat=1&lng=1"); rec.Code != 502 {
		t.Fatal(rec.Code)
	}
}

func TestAQIBands(t *testing.T) {
	for aqi, want := range map[float64]string{30: "Good", 80: "Moderate", 120: "Unhealthy for sensitive groups", 350: "Hazardous"} {
		if got, _ := aqiBand(aqi); got != want {
			t.Fatalf("%v: %s", aqi, got)
		}
	}
}
