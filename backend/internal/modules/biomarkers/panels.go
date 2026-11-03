package biomarkers

import (
	"net/http"
	"sort"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

// verifiedTrends builds a converted trend for every biomarker the profile has, plus panel categories.
func (m *Module) verifiedTrends(r *http.Request, now time.Time) (map[string]biomarker.Trend, map[string]string, error) {
	repo := observations.New(m.DB)
	profileID := chi.URLParam(r, "profileID")
	byCode, names, err := repo.LoadObservationRows(r.Context(), httpx.Owner(r), profileID, nil)
	if err != nil {
		return nil, nil, err
	}
	categories, err := repo.Categories(r.Context())
	if err != nil {
		return nil, nil, err
	}
	sex, age := repo.ProfileDemographics(r.Context(), httpx.Owner(r), profileID, now)
	trends := make(map[string]biomarker.Trend, len(byCode))
	for code, rows := range byCode {
		known, learned, err := repo.LoadKnowledge(r.Context(), code)
		if err != nil {
			return nil, nil, err
		}
		trends[code] = biomarker.BuildTrend(code, names[code], rows, false, known, learned, sex, age, now)
	}
	return trends, categories, nil
}

// biomarkerCompare shows every biomarker at two dates, in one unit, so changes across tests are comparable.
func (m *Module) biomarkerCompare(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.NotFoundProfile(w)
		return
	}
	now := time.Now().UTC()
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if to == "" {
		to = now.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		httpx.Invalid(w, "INVALID_DATE", "from must be a YYYY-MM-DD date.", "from")
		return
	}
	if _, err := time.Parse("2006-01-02", to); err != nil || to < from {
		httpx.Invalid(w, "INVALID_DATE", "to must be a YYYY-MM-DD date on or after from.", "to")
		return
	}
	trends, categories, err := m.verifiedTrends(r, now)
	if err != nil {
		httpx.WriteError(w, 500, "COMPARE_UNAVAILABLE", "The comparison is unavailable.", nil)
		return
	}
	codes := make([]string, 0, len(trends))
	for c := range trends {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	out := []biomarker.Change{}
	for _, code := range codes {
		if c := biomarker.Compare(trends[code], biomarker.CategoryOf(code, categories), from, to); c.Status != "no_data" {
			out = append(out, c)
		}
	}
	httpx.WriteJSON(w, 200, out)
}

// biomarkerCoverage reports which panels are current, stale or never measured.
func (m *Module) biomarkerCoverage(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.NotFoundProfile(w)
		return
	}
	now := time.Now().UTC()
	trends, categories, err := m.verifiedTrends(r, now)
	if err != nil {
		httpx.WriteError(w, 500, "COVERAGE_UNAVAILABLE", "The coverage report is unavailable.", nil)
		return
	}
	httpx.WriteJSON(w, 200, biomarker.Coverage(trends, categories, now))
}
