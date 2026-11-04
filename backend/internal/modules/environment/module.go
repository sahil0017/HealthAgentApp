// Package environment serves live air quality and weather for the user's coarse area (module 21)
// from Open-Meteo, plus the nearest ground station from OpenAQ when an API key is configured.
// It is stateless: coordinates are rounded on arrival and never stored or logged. It may import
// internal/platform only.
package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

const (
	weatherURL = "https://api.open-meteo.com/v1/forecast"
	airURL     = "https://air-quality-api.open-meteo.com/v1/air-quality"
	openAQURL  = "https://api.openaq.org/v3"
	cacheTTL   = 15 * time.Minute
)

type Module struct {
	module.Base
	*core.Core
	HTTP       *http.Client
	WeatherURL string
	AirURL     string
	OpenAQURL  string
	OpenAQKey  string

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	data map[string]any
}

func New(c *core.Core) *Module {
	return &Module{
		Core: c, HTTP: &http.Client{Timeout: 20 * time.Second},
		WeatherURL: weatherURL, AirURL: airURL, OpenAQURL: openAQURL,
		OpenAQKey: os.Getenv("CORTEX_OPENAQ_API_KEY"),
		cache:     map[string]cached{},
	}
}

func (m *Module) Name() string { return "environment" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/environment/now", m.now)
	r.Get("/environment/sources", m.sources)
}

func roundCoord(v float64) float64 { return math.Round(v/0.05) * 0.05 }

// aqiBand follows the US EPA AQI categories, which are a published public standard.
func aqiBand(aqi float64) (string, string) {
	switch {
	case aqi <= 50:
		return "Good", "Air quality is satisfactory."
	case aqi <= 100:
		return "Moderate", "People unusually sensitive to air pollution may want to limit long outdoor exertion."
	case aqi <= 150:
		return "Unhealthy for sensitive groups", "People with asthma, heart or lung disease, children and older adults should reduce prolonged outdoor exertion."
	case aqi <= 200:
		return "Unhealthy", "Everyone may begin to feel effects. Reduce prolonged outdoor exertion."
	case aqi <= 300:
		return "Very unhealthy", "Avoid outdoor exertion; sensitive groups should stay indoors."
	}
	return "Hazardous", "Avoid all outdoor exertion."
}

func (m *Module) sources(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, 200, map[string]any{"sources": []map[string]any{
		{"category": "air_quality", "name": "Open-Meteo Air Quality", "scope": "global", "available": true},
		{"category": "weather", "name": "Open-Meteo Weather", "scope": "global", "available": true},
		{"category": "station", "name": "OpenAQ ground stations", "scope": "global", "available": m.OpenAQKey != "",
			"reason": map[bool]string{true: "", false: "Set CORTEX_OPENAQ_API_KEY to show the nearest monitoring station."}[m.OpenAQKey != ""]},
	}})
}

func (m *Module) fetch(ctx context.Context, u string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SehatApp/1.0 (personal health app)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

func (m *Module) now(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var lat, lng float64
	_, e1 := fmt.Sscanf(q.Get("lat"), "%f", &lat)
	_, e2 := fmt.Sscanf(q.Get("lng"), "%f", &lng)
	if e1 != nil || e2 != nil || math.Abs(lat) > 90 || math.Abs(lng) > 180 {
		httpx.WriteError(w, 400, "INVALID_AREA", "A latitude and longitude are required.", nil)
		return
	}
	lat, lng = roundCoord(lat), roundCoord(lng)
	key := fmt.Sprintf("%.2f:%.2f", lat, lng)
	m.mu.Lock()
	c, hit := m.cache[key]
	m.mu.Unlock()
	if hit && time.Since(c.at) < cacheTTL {
		httpx.WriteJSON(w, 200, c.data)
		return
	}
	ll := url.Values{"latitude": {fmt.Sprintf("%.2f", lat)}, "longitude": {fmt.Sprintf("%.2f", lng)}}
	wq := url.Values{"current": {"temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,weather_code"}}
	aq := url.Values{"current": {"us_aqi,pm2_5,pm10,ozone,nitrogen_dioxide,uv_index"}}
	for k, v := range ll {
		wq[k], aq[k] = v, v
	}
	air, airErr := m.fetch(r.Context(), m.AirURL+"?"+aq.Encode(), nil)
	wx, wxErr := m.fetch(r.Context(), m.WeatherURL+"?"+wq.Encode(), nil)
	if airErr != nil && wxErr != nil {
		httpx.WriteError(w, 502, "ENVIRONMENT_UNAVAILABLE", "Air quality and weather could not be loaded right now.", nil)
		return
	}
	out := map[string]any{"source": "Open-Meteo", "lat": lat, "lng": lng}
	if airErr == nil {
		cur, _ := air["current"].(map[string]any)
		out["air"] = map[string]any{"current": cur, "units": air["current_units"]}
		if v, ok := cur["us_aqi"].(float64); ok {
			cat, adv := aqiBand(v)
			out["air_category"], out["air_advice"] = cat, adv
			out["air_standard"] = "US EPA AQI categories"
		}
	}
	if wxErr == nil {
		out["weather"] = map[string]any{"current": wx["current"], "units": wx["current_units"]}
	}
	if m.OpenAQKey != "" {
		if st := m.station(r.Context(), lat, lng); st != nil {
			out["station"] = st
		}
	}
	m.mu.Lock()
	if len(m.cache) > 500 {
		m.cache = map[string]cached{}
	}
	m.cache[key] = cached{at: time.Now(), data: out}
	m.mu.Unlock()
	httpx.WriteJSON(w, 200, out)
}

// station returns the nearest OpenAQ monitoring station and its latest readings, or nil.
func (m *Module) station(ctx context.Context, lat, lng float64) map[string]any {
	h := map[string]string{"X-API-Key": m.OpenAQKey}
	locs, err := m.fetch(ctx, fmt.Sprintf("%s/locations?coordinates=%.4f,%.4f&radius=25000&limit=1", m.OpenAQURL, lat, lng), h)
	if err != nil {
		return nil
	}
	res, _ := locs["results"].([]any)
	if len(res) == 0 {
		return nil
	}
	loc, _ := res[0].(map[string]any)
	id, _ := loc["id"].(float64)
	names := map[float64]string{}
	units := map[float64]string{}
	sensors, _ := loc["sensors"].([]any)
	for _, s := range sensors {
		sm, _ := s.(map[string]any)
		sid, _ := sm["id"].(float64)
		p, _ := sm["parameter"].(map[string]any)
		names[sid], _ = p["name"].(string)
		units[sid], _ = p["units"].(string)
	}
	latest, err := m.fetch(ctx, fmt.Sprintf("%s/locations/%d/latest", m.OpenAQURL, int64(id)), h)
	if err != nil {
		return nil
	}
	readings := []map[string]any{}
	lr, _ := latest["results"].([]any)
	for _, x := range lr {
		xm, _ := x.(map[string]any)
		sid, _ := xm["sensorsId"].(float64)
		if names[sid] == "" {
			continue
		}
		readings = append(readings, map[string]any{"parameter": names[sid], "value": xm["value"], "unit": units[sid]})
	}
	return map[string]any{"name": loc["name"], "source": "OpenAQ", "readings": readings}
}
