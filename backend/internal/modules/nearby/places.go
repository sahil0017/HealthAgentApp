package nearby

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"cortex/backend/internal/platform/httpx"
)

// placeKinds maps a care category to the OpenStreetMap tags that describe it.
var placeKinds = map[string][]string{
	"doctor":   {"nwr[amenity=doctors]", "nwr[healthcare=doctor]"},
	"clinic":   {"nwr[amenity=clinic]", "nwr[healthcare=clinic]"},
	"pharmacy": {"nwr[amenity=pharmacy]"},
	"lab":      {"nwr[healthcare=laboratory]", "nwr[amenity=laboratory]"},
}

// placeTerms are the Nominatim categories used when Overpass is unreachable.
var placeTerms = map[string][]string{
	"doctor":   {"doctors"},
	"clinic":   {"clinic"},
	"pharmacy": {"pharmacy"},
	"lab":      {"diagnostic laboratory"},
}

type place struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Speciality   string  `json:"speciality,omitempty"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	DistanceKm   float64 `json:"distance_km"`
	Phone        string  `json:"phone,omitempty"`
	Website      string  `json:"website,omitempty"`
	OpeningHours string  `json:"opening_hours,omitempty"`
	Address      string  `json:"address,omitempty"`
}

// places lists doctors, clinics, pharmacies or labs near an area. They are community-mapped
// listings, not registry-verified, and the response says so.
func (m *Module) places(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	filters, ok := placeKinds[kind]
	lat, e1 := strconv.ParseFloat(q.Get("lat"), 64)
	lng, e2 := strconv.ParseFloat(q.Get("lng"), 64)
	radius := 10.0
	if v := q.Get("radius_km"); v != "" {
		radius, _ = strconv.ParseFloat(v, 64)
	}
	if !ok || e1 != nil || e2 != nil || math.Abs(lat) > 90 || math.Abs(lng) > 180 || radius < 1 || radius > 50 {
		httpx.WriteError(w, 400, "INVALID_AREA", "A kind (doctor, clinic, pharmacy or lab), a latitude, longitude and a radius of 1-50 km are required.", nil)
		return
	}
	lat, lng = roundCoord(lat), roundCoord(lng)
	key := fmt.Sprintf("p:%s:%.3f:%.3f:%.0f", kind, lat, lng, radius)
	resp := func(v any) map[string]any {
		return map[string]any{"places": v, "kind": kind, "source": "OpenStreetMap", "verified": false,
			"note": "Listed by OpenStreetMap contributors. Not verified against a medical registry and not a recommendation."}
	}
	if v, hit := m.fromCache(key); hit {
		httpx.WriteJSON(w, 200, resp(v))
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[out:json][timeout:25];(")
	for _, f := range filters {
		fmt.Fprintf(&sb, "%s(around:%d,%.5f,%.5f);", f, int(radius*1000), lat, lng)
	}
	sb.WriteString(");out center 80;")
	body, err := m.fetchArea(r.Context(), sb.String(), lat, lng, radius, placeTerms[kind], nil)
	if err != nil {
		if v, ok := m.stale(key); ok {
			httpx.WriteJSON(w, 200, resp(v))
			return
		}
		httpx.WriteError(w, 502, "PLACES_UNAVAILABLE", "Nearby places could not be loaded right now.", nil)
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
		httpx.WriteError(w, 502, "PLACES_UNAVAILABLE", "Nearby places could not be loaded right now.", nil)
		return
	}
	out := []place{}
	for _, e := range in.Elements {
		name := strings.TrimSpace(e.Tags["name"])
		if name == "" {
			continue
		}
		la, lo := e.Lat, e.Lon
		if e.Center != nil {
			la, lo = e.Center.Lat, e.Center.Lon
		}
		first := func(keys ...string) string {
			for _, k := range keys {
				if v := strings.TrimSpace(e.Tags[k]); v != "" {
					return v
				}
			}
			return ""
		}
		out = append(out, place{
			ID: fmt.Sprintf("%s/%d", e.Type, e.ID), Kind: kind, Name: name,
			Speciality:   strings.ReplaceAll(first("healthcare:speciality", "speciality"), ";", ", "),
			Lat:          la,
			Lng:          lo,
			DistanceKm:   math.Round(haversineKm(lat, lng, la, lo)*10) / 10,
			Phone:        first("phone", "contact:phone"),
			Website:      first("website", "contact:website"),
			OpeningHours: first("opening_hours"),
			Address:      strings.Join(nonEmpty(e.Tags["addr:street"], e.Tags["addr:city"]), ", "),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceKm < out[j].DistanceKm })
	if len(out) > 40 {
		out = out[:40]
	}
	m.toCache(key, out)
	httpx.WriteJSON(w, 200, resp(out))
}
