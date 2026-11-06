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

// ReportResult is one test value inside a report, with how it moved since the same test in the previous report.
type ReportResult struct {
	BiomarkerCode string               `json:"biomarker_code"`
	DisplayName   string               `json:"display_name"`
	Category      string               `json:"category"`
	CategoryLabel string               `json:"category_label"`
	Value         float64              `json:"value"`
	Unit          string               `json:"unit"`
	ObservedAt    string               `json:"observed_at"`
	Flag          string               `json:"flag"`
	Range         *biomarker.RangeInfo `json:"range"`
	Previous      *float64             `json:"previous_value"`
	Change        *float64             `json:"change"`
	PreviousOn    string               `json:"previous_observed_at,omitempty"`
}

// ReportGroup is one uploaded report with every result read from it.
type ReportGroup struct {
	DocumentID string         `json:"document_id"`
	Title      string         `json:"title"`
	Lab        string         `json:"lab,omitempty"`
	FileName   string         `json:"file_name"`
	ReportDate string         `json:"report_date"`
	UploadedAt string         `json:"uploaded_at,omitempty"`
	Total      int            `json:"total"`
	Outside    int            `json:"outside_range"`
	Inside     int            `json:"in_range"`
	NoRange    int            `json:"no_range"`
	Changed    int            `json:"changed_since_previous"`
	Results    []ReportResult `json:"results"`
}

func outsideRange(flag string) bool { return flag == "above_range" || flag == "below_range" }

// byReport groups every verified result under the report it was read from, newest report first, so a person with
// several uploads can see which test came from which report and date.
func (m *Module) byReport(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	repo := observations.New(m.DB)
	byCode, names, err := repo.LoadObservationRows(r.Context(), httpx.Owner(r), profileID, nil)
	if err != nil {
		httpx.WriteError(w, 500, "REPORTS_UNAVAILABLE", "Your reports could not be read right now.", nil)
		return
	}
	categories, err := repo.Categories(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "REPORTS_UNAVAILABLE", "Your reports could not be read right now.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := repo.ProfileDemographics(r.Context(), httpx.Owner(r), profileID, now)

	groups := map[string]*ReportGroup{}
	for code, rows := range byCode {
		known, learned, err := repo.LoadKnowledge(r.Context(), code)
		if err != nil {
			httpx.WriteError(w, 500, "REPORTS_UNAVAILABLE", "Your reports could not be read right now.", nil)
			return
		}
		t := biomarker.BuildTrend(code, names[code], rows, false, known, learned, sex, age, now)
		cat := biomarker.CategoryFor(code, t.DisplayName, categories)
		for i, p := range t.Points {
			if p.DocumentID == "" {
				continue
			}
			g := groups[p.DocumentID]
			if g == nil {
				g = &ReportGroup{DocumentID: p.DocumentID, ReportDate: p.ObservedAt, Results: []ReportResult{}}
				groups[p.DocumentID] = g
			}
			if p.ObservedAt > g.ReportDate {
				g.ReportDate = p.ObservedAt
			}
			res := ReportResult{BiomarkerCode: code, DisplayName: t.DisplayName, Category: cat, CategoryLabel: biomarker.CategoryLabel(cat), Value: p.Value, Unit: p.Unit, ObservedAt: p.ObservedAt, Flag: p.Flag, Range: p.Range}
			if prev, ok := previousReportPoint(t.Points, i); ok {
				change := p.Value - prev.Value
				res.Previous, res.Change, res.PreviousOn = &prev.Value, &change, prev.ObservedAt
				g.Changed++
			}
			g.Results = append(g.Results, res)
			g.Total++
			switch {
			case outsideRange(p.Flag):
				g.Outside++
			case p.Flag == "within_range":
				g.Inside++
			default:
				g.NoRange++
			}
		}
	}

	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		rows, err := m.DB.Pool.Query(r.Context(), `SELECT id::text,file_name,created_at,COALESCE(extraction->'ai'->>'provider',''),COALESCE(extraction->'ai'->>'report_date','') FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND id::text = ANY($3)`, profileID, httpx.Owner(r), ids)
		if err != nil {
			httpx.WriteError(w, 500, "REPORTS_UNAVAILABLE", "Your reports could not be read right now.", nil)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id, file, lab, date string
			var created time.Time
			if err := rows.Scan(&id, &file, &created, &lab, &date); err != nil {
				httpx.WriteError(w, 500, "REPORTS_UNAVAILABLE", "Your reports could not be read right now.", nil)
				return
			}
			g := groups[id]
			g.FileName, g.Lab, g.UploadedAt = file, strings.TrimSpace(lab), created.UTC().Format(time.RFC3339)
			if len(date) == 10 {
				g.ReportDate = date
			}
		}
	}

	out := make([]ReportGroup, 0, len(groups))
	for _, g := range groups {
		g.Title = reportTitle(g)
		sort.SliceStable(g.Results, func(i, j int) bool {
			a, b := outsideRange(g.Results[i].Flag), outsideRange(g.Results[j].Flag)
			if a != b {
				return a
			}
			return g.Results[i].DisplayName < g.Results[j].DisplayName
		})
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ReportDate != out[j].ReportDate {
			return out[i].ReportDate > out[j].ReportDate
		}
		return out[i].UploadedAt > out[j].UploadedAt
	})
	httpx.WriteJSON(w, 200, out)
}

// reportTitle names a report by its lab and date; a bare file name is only the fallback.
func reportTitle(g *ReportGroup) string {
	if g.Lab != "" {
		return g.Lab
	}
	name := strings.TrimSuffix(g.FileName, ".pdf")
	if name == "" || len(name) > 40 {
		return "Lab report"
	}
	return name
}

// previousReportPoint compares laboratory readings from different, dated reports.
// Same-day reports have no reliable ordering.
func previousReportPoint(points []biomarker.TrendPoint, index int) (biomarker.TrendPoint, bool) {
	current := points[index]
	if current.DocumentID == "" || current.MeasurementKind != "lab" || current.ObservedAt == "" || current.DateInferred {
		return biomarker.TrendPoint{}, false
	}
	var best biomarker.TrendPoint
	found := false
	for i := index - 1; i >= 0; i-- {
		p := points[i]
		if p.DocumentID != "" && p.DocumentID != current.DocumentID && !p.DateInferred &&
			p.MeasurementKind == "lab" && p.ObservedAt < current.ObservedAt {
			if !found {
				best, found = p, true
			} else if p.ObservedAt < best.ObservedAt {
				break
			} else if p.DocumentID != best.DocumentID || p.Value != best.Value {
				return biomarker.TrendPoint{}, false
			}
		}
	}
	return best, found
}
