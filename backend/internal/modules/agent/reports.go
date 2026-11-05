package agent

import (
	"fmt"
	"sort"
	"strings"

	"cortex/backend/internal/domain/biomarker"
)

// report is one uploaded document and the verified results read from it.
type report struct {
	ID, Title, Date string
	Lines           []reportLine
}

type reportLine struct {
	Name, Unit, Flag, Date string
	Value                  float64
	Point                  biomarker.TrendPoint
	Prev                   *biomarker.TrendPoint
}

// loadReports groups the already-loaded trend points by the document they came from. Lab names and dates come from
// the document's AI reading when there is one.
func (l *labs) loadReports() {
	l.reports = nil
	byDoc := map[string]*report{}
	for _, c := range l.codes() {
		t := l.trends[c]
		for i, p := range t.Points {
			if p.DocumentID == "" {
				continue
			}
			r := byDoc[p.DocumentID]
			if r == nil {
				r = &report{ID: p.DocumentID, Date: p.ObservedAt}
				byDoc[p.DocumentID] = r
			}
			if p.ObservedAt > r.Date {
				r.Date = p.ObservedAt
			}
			line := reportLine{Name: t.DisplayName, Unit: t.CanonicalUnit, Flag: p.Flag, Value: p.Value, Date: p.ObservedAt, Point: p}
			if i > 0 {
				prev := t.Points[i-1]
				line.Prev = &prev
			}
			r.Lines = append(r.Lines, line)
		}
	}
	if len(byDoc) == 0 {
		return
	}
	ids := make([]string, 0, len(byDoc))
	for id := range byDoc {
		ids = append(ids, id)
	}
	rows, err := l.m.DB.Pool.Query(l.ctx, `SELECT id::text,file_name,COALESCE(extraction->'ai'->>'provider',''),COALESCE(extraction->'ai'->>'report_date','') FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND id::text = ANY($3)`, l.profile, l.owner, ids)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, file, lab, date string
			if rows.Scan(&id, &file, &lab, &date) != nil {
				continue
			}
			r := byDoc[id]
			r.Title = strings.TrimSpace(lab)
			if r.Title == "" {
				r.Title = strings.TrimSuffix(file, ".pdf")
			}
			if len(date) == 10 {
				r.Date = date
			}
		}
	}
	for _, r := range byDoc {
		if r.Title == "" {
			r.Title = "Lab report"
		}
		sort.SliceStable(r.Lines, func(i, j int) bool { return r.Lines[i].Name < r.Lines[j].Name })
		l.reports = append(l.reports, r)
	}
	sort.SliceStable(l.reports, func(i, j int) bool {
		if l.reports[i].Date != l.reports[j].Date {
			return l.reports[i].Date > l.reports[j].Date
		}
		return l.reports[i].ID < l.reports[j].ID
	})
}

func (r *report) label() string {
	return fmt.Sprintf("%s, %s", r.Title, r.Date)
}

// findReport resolves what the person or the model called a report: its id, "latest", "previous", the lab name
// or part of the date.
func (l *labs) findReport(q string) (*report, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || len(l.reports) == 0 {
		return nil, false
	}
	switch q {
	case "latest", "newest", "last", "most recent", "recent":
		return l.reports[0], true
	case "previous", "earlier", "older", "before":
		if len(l.reports) > 1 {
			return l.reports[1], true
		}
		return nil, false
	}
	for _, r := range l.reports {
		if strings.ToLower(r.ID) == q {
			return r, true
		}
	}
	for _, r := range l.reports {
		if strings.Contains(strings.ToLower(r.Title), q) || strings.Contains(q, strings.ToLower(r.Title)) || strings.Contains(r.Date, q) {
			return r, true
		}
	}
	return nil, false
}

func (l *labs) listReportsText() string {
	if len(l.reports) == 0 {
		return "No lab reports with verified results are in the record."
	}
	lines := make([]string, 0, len(l.reports))
	for i, r := range l.reports {
		out := 0
		for _, ln := range r.Lines {
			if ln.Flag == "above_range" || ln.Flag == "below_range" {
				out++
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s (id %s): %d results, %d outside range.", i+1, r.label(), r.ID, len(r.Lines), out))
	}
	return "Reports on file, newest first:\n" + strings.Join(lines, "\n")
}

func reportText(r *report) string {
	lines := make([]string, 0, len(r.Lines))
	for _, ln := range r.Lines {
		s := fmt.Sprintf("%s: %v %s (%s, %s)", ln.Name, ln.Value, ln.Unit, flagWord(ln.Flag), describeRange(ln.Point))
		if ln.Prev != nil {
			s += fmt.Sprintf("; previously %v on %s, change %+.4g", ln.Prev.Value, ln.Prev.ObservedAt, ln.Value-ln.Prev.Value)
		}
		lines = append(lines, s+".")
	}
	return fmt.Sprintf("Report %s (id %s), %d results:\n%s", r.label(), r.ID, len(r.Lines), strings.Join(lines, "\n"))
}

// reportScope tells the model which report the person opened the chat from.
func (l *labs) reportScope() string {
	if l.scope == nil {
		return ""
	}
	return fmt.Sprintf(" The person opened this chat from one report: %q (id %s). Call get_report with that id first and answer from that report only; mention its name and date.", l.scope.label(), l.scope.ID)
}

func (l *labs) reportSnapshot() []map[string]string {
	out := make([]map[string]string, 0, len(l.reports))
	for _, r := range l.reports {
		out = append(out, map[string]string{"id": r.ID, "title": r.Title, "date": r.Date, "out": reportText(r)})
	}
	return out
}
