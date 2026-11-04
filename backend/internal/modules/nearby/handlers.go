package nearby

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"
)

var tagRE = regexp.MustCompile(`<[^>]*>`)

// roundCoord snaps a coordinate to a ~5 km grid so the server never works with a precise position.
func roundCoord(v float64) float64 { return math.Round(v/0.05) * 0.05 }

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

func (m *Module) sources(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, 200, map[string]any{"sources": []map[string]any{
		{"category": "alerts", "name": "WHO Disease Outbreak News", "scope": "global", "available": true},
		{"category": "hospitals", "name": "OpenStreetMap", "scope": "global", "available": true},
		{"category": "doctors", "name": "", "scope": "", "available": false,
			"reason": "Doctor directories are national registries; none is connected yet."},
	}})
}

type alert struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	PublishedAt string `json:"published_at"`
	URL         string `json:"url"`
	Source      string `json:"source"`
	InCountry   bool   `json:"in_country"`
}

func (m *Module) alerts(w http.ResponseWriter, r *http.Request) {
	country := strings.TrimSpace(r.URL.Query().Get("country"))
	if len(country) > 80 {
		httpx.WriteError(w, 400, "INVALID_COUNTRY", "The country name is too long.", nil)
		return
	}
	items, err := m.who(r.Context())
	if err != nil {
		httpx.WriteError(w, 502, "ALERTS_UNAVAILABLE", "Outbreak alerts could not be loaded right now.", nil)
		return
	}
	out := make([]alert, 0, len(items))
	for _, a := range items {
		a.InCountry = country != "" && strings.Contains(strings.ToLower(a.Title+" "+a.Summary), strings.ToLower(country))
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].InCountry && !out[j].InCountry })
	httpx.WriteJSON(w, 200, map[string]any{"alerts": out, "source": "WHO Disease Outbreak News", "country": country})
}

func (m *Module) who(ctx context.Context) ([]alert, error) {
	if v, ok := m.fromCache("who"); ok {
		return v.([]alert), nil
	}
	q := url.Values{"sf_culture": {"en"}, "$orderby": {"PublicationDateAndTime desc"}, "$top": {"20"},
		"$select": {"Title,PublicationDateAndTime,UrlName,Summary"}}
	body, err := m.get(ctx, m.WHOURL+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var in struct {
		Value []struct {
			Title   string `json:"Title"`
			Date    string `json:"PublicationDateAndTime"`
			URLName string `json:"UrlName"`
			Summary string `json:"Summary"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	out := make([]alert, 0, len(in.Value))
	for _, v := range in.Value {
		out = append(out, alert{ID: v.URLName, Title: strings.TrimSpace(v.Title), Summary: clip(plain(v.Summary), 360),
			PublishedAt: v.Date, URL: "https://www.who.int/emergencies/disease-outbreak-news/item/" + v.URLName, Source: "WHO"})
	}
	m.toCache("who", out)
	return out, nil
}

func plain(s string) string {
	s = html.UnescapeString(tagRE.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

type hospital struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	DistanceKm float64 `json:"distance_km"`
	Emergency  bool    `json:"emergency"`
	Phone      string  `json:"phone,omitempty"`
	Address    string  `json:"address,omitempty"`
}

func (m *Module) hospitals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
	lng, e2 := strconv.ParseFloat(q.Get("lng"), 64)
	radius := 10.0
	if v := q.Get("radius_km"); v != "" {
		radius, _ = strconv.ParseFloat(v, 64)
	}
	if e1 != nil || e2 != nil || math.Abs(lat) > 90 || math.Abs(lng) > 180 || radius < 1 || radius > 50 {
		httpx.WriteError(w, 400, "INVALID_AREA", "A latitude, longitude and a radius of 1-50 km are required.", nil)
		return
	}
	lat, lng = roundCoord(lat), roundCoord(lng)
	key := fmt.Sprintf("h:%.2f:%.2f:%.0f", lat, lng, radius)
	if v, ok := m.fromCache(key); ok {
		httpx.WriteJSON(w, 200, map[string]any{"hospitals": v, "source": "OpenStreetMap"})
		return
	}
	query := fmt.Sprintf(`[out:json][timeout:25];nwr[amenity=hospital](around:%d,%.5f,%.5f);out center 60;`, int(radius*1000), lat, lng)
	body, err := m.post(r.Context(), m.OverpassURL, url.Values{"data": {query}})
	if err != nil {
		httpx.WriteError(w, 502, "HOSPITALS_UNAVAILABLE", "Nearby hospitals could not be loaded right now.", nil)
		return
	}
	var in struct {
		Elements []struct {
			Type   string  `json:"type"`
			ID     int64   `json:"id"`
			Lat    float64 `json:"lat"`
			Lon    float64 `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		httpx.WriteError(w, 502, "HOSPITALS_UNAVAILABLE", "Nearby hospitals could not be loaded right now.", nil)
		return
	}
	out := []hospital{}
	for _, e := range in.Elements {
		name := strings.TrimSpace(e.Tags["name"])
		if name == "" {
			continue
		}
		la, lo := e.Lat, e.Lon
		if e.Center != nil {
			la, lo = e.Center.Lat, e.Center.Lon
		}
		phone := e.Tags["phone"]
		if phone == "" {
			phone = e.Tags["contact:phone"]
		}
		out = append(out, hospital{ID: fmt.Sprintf("%s/%d", e.Type, e.ID), Name: name, Lat: la, Lng: lo,
			DistanceKm: math.Round(haversineKm(lat, lng, la, lo)*10) / 10, Emergency: e.Tags["emergency"] == "yes",
			Phone: phone, Address: strings.Join(nonEmpty(e.Tags["addr:street"], e.Tags["addr:city"]), ", ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceKm < out[j].DistanceKm })
	if len(out) > 30 {
		out = out[:30]
	}
	m.toCache(key, out)
	httpx.WriteJSON(w, 200, map[string]any{"hospitals": out, "source": "OpenStreetMap"})
}

func nonEmpty(v ...string) []string {
	var out []string
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func (m *Module) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	return m.do(req)
}

func (m *Module) post(ctx context.Context, u string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return m.do(req)
}

func (m *Module) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", userAgent)
	ctx, cancel := context.WithTimeout(req.Context(), 25*time.Second)
	defer cancel()
	resp, err := m.HTTP.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
