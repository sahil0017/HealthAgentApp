package biomarkers

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

func (m *Module) biomarkerTrend(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID, code := chi.URLParam(r, "profileID"), strings.ToUpper(chi.URLParam(r, "code"))
	includeUnverified := r.URL.Query().Get("include_unverified") == "true"
	byCode, names, err := observations.New(m.DB).LoadObservationRows(r.Context(), httpx.Owner(r), profileID, &code)
	if err != nil {
		httpx.WriteError(w, 500, "TREND_UNAVAILABLE", "The trend is unavailable.", nil)
		return
	}
	if len(byCode[code]) == 0 {
		var exists bool
		_ = m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM biomarker_catalog WHERE code=$1)`, code).Scan(&exists)
		if !exists {
			httpx.WriteError(w, 404, "BIOMARKER_NOT_FOUND", "The biomarker is not in the catalog.", nil)
			return
		}
		var display string
		_ = m.DB.Pool.QueryRow(r.Context(), `SELECT display_name FROM biomarker_catalog WHERE code=$1`, code).Scan(&display)
		names[code] = display
	}
	known, learned, err := observations.New(m.DB).LoadKnowledge(r.Context(), code)
	if err != nil {
		httpx.WriteError(w, 500, "TREND_UNAVAILABLE", "The trend is unavailable.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := observations.New(m.DB).ProfileDemographics(r.Context(), httpx.Owner(r), profileID, now)
	httpx.WriteJSON(w, 200, biomarker.BuildTrend(code, names[code], byCode[code], includeUnverified, known, learned, sex, age, now))
}

type BiomarkerSummary struct {
	BiomarkerCode string                `json:"biomarker_code"`
	DisplayName   string                `json:"display_name"`
	Category      string                `json:"category"`
	Latest        *biomarker.TrendPoint `json:"latest"`
	Previous      *float64              `json:"previous_value"`
	Change        *float64              `json:"change"`
	Unit          string                `json:"unit"`
	Trend         biomarker.TrendResult `json:"trend"`
	Cadence       biomarker.CadenceInfo `json:"cadence"`
	Readings      int                   `json:"verified_readings"`
	Unverified    int                   `json:"unverified_count"`
	Excluded      int                   `json:"excluded_count"`
	Kinds         []string              `json:"measurement_kinds"`
}

func (m *Module) biomarkerSummaries(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	byCode, names, err := observations.New(m.DB).LoadObservationRows(r.Context(), httpx.Owner(r), profileID, nil)
	if err != nil {
		httpx.WriteError(w, 500, "SUMMARY_UNAVAILABLE", "The biomarker summary is unavailable.", nil)
		return
	}
	categories, err := observations.New(m.DB).Categories(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "SUMMARY_UNAVAILABLE", "The biomarker summary is unavailable.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := observations.New(m.DB).ProfileDemographics(r.Context(), httpx.Owner(r), profileID, now)
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	out := []BiomarkerSummary{}
	for _, code := range codes {
		known, learned, err := observations.New(m.DB).LoadKnowledge(r.Context(), code)
		if err != nil {
			httpx.WriteError(w, 500, "SUMMARY_UNAVAILABLE", "The biomarker summary is unavailable.", nil)
			return
		}
		t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		s := BiomarkerSummary{BiomarkerCode: code, DisplayName: t.DisplayName, Category: biomarker.CategoryOf(code, categories), Unit: t.CanonicalUnit, Trend: t.Trend, Cadence: t.Cadence, Readings: len(t.Points), Unverified: t.Unverified, Excluded: len(t.Excluded), Kinds: t.Kinds}
		if n := len(t.Points); n > 0 {
			latest := t.Points[n-1]
			s.Latest = &latest
			if n > 1 {
				prev := t.Points[n-2].Value
				change := latest.Value - prev
				s.Previous, s.Change = &prev, &change
			}
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, 200, out)
}
