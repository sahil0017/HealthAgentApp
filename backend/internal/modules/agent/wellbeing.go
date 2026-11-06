package agent

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/domain/lifestyle"
	"cortex/backend/internal/domain/wellbeing"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

type planAction struct {
	Type string `json:"type"` // follow_ups, biomarker, log or care_plan
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

type planTask struct {
	ID     string     `json:"id"`
	Kind   string     `json:"kind"`
	Title  string     `json:"title"`
	Why    string     `json:"why"`
	Action planAction `json:"action"`
}

type habitView struct {
	wellbeing.Series
	Name string `json:"name"`
	Unit string `json:"unit"`
}

func fmtVal(v float64) string { return fmt.Sprintf("%g", math.Round(v*10)/10) }

// wellbeingPlan returns the Summary dashboard: a score built from the person's own labs, habits and care plan,
// each habit's weekly projection, and a short ordered list of things to do today with the data behind each.
func (m *Module) wellbeingPlan(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	today, err := time.Parse("2006-01-02", r.URL.Query().Get("day"))
	if err != nil {
		today = time.Now().UTC().Truncate(24 * time.Hour)
	}

	// Habits: combine each day's entries per category, then analyse against the person's own baseline.
	raw := map[string]map[string][]float64{}
	if rows, err := m.DB.Pool.Query(ctx, `SELECT category, to_char(logged_on,'YYYY-MM-DD'), value FROM lifestyle_logs
		WHERE profile_id=$1 AND owner_id=$2 AND logged_on > $3::date-30 AND logged_on <= $3::date`, profileID, owner, today.Format("2006-01-02")); err == nil {
		for rows.Next() {
			var cat, day string
			var v float64
			if err := rows.Scan(&cat, &day, &v); err != nil {
				rows.Close()
				httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
				return
			}
			if raw[cat] == nil {
				raw[cat] = map[string][]float64{}
			}
			raw[cat][day] = append(raw[cat][day], v)
		}
		if rows.Err() != nil {
			rows.Close()
			httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
			return
		}
		rows.Close()
	} else {
		httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
		return
	}
	habits := []habitView{}
	series := []wellbeing.Series{}
	for _, c := range lifestyle.Categories() {
		daily := map[string]float64{}
		for d, vs := range raw[c.Code] {
			daily[d] = c.Combine(vs)
		}
		s := wellbeing.Analyse(c.Code, daily, today, c.Min, c.Max)
		series = append(series, s)
		habits = append(habits, habitView{Series: s, Name: shortName(c.Name), Unit: c.Unit})
	}

	// Labs.
	labs, err := m.labLines(ctx, owner, profileID, "")
	if err != nil {
		httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
		return
	}
	inRange, unrated := 0, 0
	byCode := map[string]labLine{}
	var flagged []labLine
	for _, l := range labs {
		byCode[l.Code] = l
		if isOut(l.Flag) {
			flagged = append(flagged, l)
		} else if l.Flag == "within_range" {
			inRange++
		} else {
			unrated++
		}
	}

	// Care plan.
	type goal struct {
		ID, Code, Title, Unit string
		Min, Max              *float64
	}
	var goals []goal
	if rows, err := m.DB.Pool.Query(ctx, `SELECT id::text,biomarker_code,title,unit,target_min::float8,target_max::float8 FROM goals WHERE profile_id=$1 AND owner_id=$2 AND status='active' ORDER BY created_at,id`, profileID, owner); err == nil {
		for rows.Next() {
			var g goal
			if err := rows.Scan(&g.ID, &g.Code, &g.Title, &g.Unit, &g.Min, &g.Max); err != nil {
				rows.Close()
				httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
				return
			}
			goals = append(goals, g)
		}
		if rows.Err() != nil {
			rows.Close()
			httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
			return
		}
		rows.Close()
	} else {
		httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
		return
	}
	type measuredGoal struct {
		goal
		Value float64
	}
	var off []measuredGoal
	onTarget, assessed := 0, 0
	repo := observations.New(m.DB)
	for _, g := range goals {
		l, ok := byCode[g.Code]
		if !ok {
			continue
		}
		known, _, err := repo.LoadKnowledge(ctx, g.Code)
		if err != nil {
			httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
			return
		}
		value, ok := biomarker.ConvertValue(l.Value, l.Unit, g.Unit, known)
		if !ok {
			continue
		}
		assessed++
		if (g.Min == nil || value >= *g.Min) && (g.Max == nil || value <= *g.Max) {
			onTarget++
		} else {
			off = append(off, measuredGoal{goal: g, Value: value})
		}
	}
	type followUp struct {
		ID, Title string
		Days      int
	}
	var due []followUp
	open := 0
	if rows, err := m.DB.Pool.Query(ctx, `SELECT id::text, title, (due_on - $3::date) FROM follow_ups
		WHERE profile_id=$1 AND owner_id=$2 AND status='open' AND due_on IS NOT NULL`, profileID, owner, today.Format("2006-01-02")); err == nil {
		for rows.Next() {
			var f followUp
			if err := rows.Scan(&f.ID, &f.Title, &f.Days); err != nil {
				rows.Close()
				httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
				return
			}
			open++
			if f.Days <= 7 {
				due = append(due, f)
			}
		}
		if rows.Err() != nil {
			rows.Close()
			httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
			return
		}
		rows.Close()
	} else {
		httpx.WriteError(w, 500, "WELLBEING_UNAVAILABLE", "Wellbeing data could not be loaded.", nil)
		return
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Days < due[j].Days })

	// Score parts, each only when there is data behind it.
	parts := []wellbeing.Part{}
	rated := inRange + len(flagged)
	if rated > 0 {
		parts = append(parts, wellbeing.Part{Key: "labs", Score: math.Round(100 * float64(inRange) / float64(rated)), Weight: 40})
	}
	if hs, ok := wellbeing.HabitScore(series); ok {
		parts = append(parts, wellbeing.Part{Key: "habits", Score: math.Round(hs), Weight: 40})
	}
	var care []float64
	overdue := 0
	for _, f := range due {
		if f.Days < 0 {
			overdue++
		}
	}
	if assessed > 0 {
		care = append(care, 100*float64(onTarget)/float64(assessed))
	}
	if open > 0 {
		care = append(care, 100*float64(open-overdue)/float64(open))
	}
	if len(care) > 0 {
		s := 0.0
		for _, c := range care {
			s += c
		}
		parts = append(parts, wellbeing.Part{Key: "care", Score: math.Round(s / float64(len(care))), Weight: 20})
	}

	// Today's plan, most important first.
	tasks := []planTask{}
	for _, f := range due {
		why := fmt.Sprintf("Due in %d days.", f.Days)
		switch {
		case f.Days < 0:
			why = fmt.Sprintf("Overdue by %d days.", -f.Days)
		case f.Days == 0:
			why = "Due today."
		}
		tasks = append(tasks, planTask{ID: "fu-" + f.ID, Kind: "follow_up", Title: f.Title, Why: why, Action: planAction{Type: "follow_ups"}})
		if len(tasks) == 2 {
			break
		}
	}
	for i, g := range off {
		if i == 2 {
			break
		}
		l := byCode[g.Code]
		tasks = append(tasks, planTask{ID: "goal-" + g.ID, Kind: "goal", Title: "Move " + g.Title + " toward target",
			Why: fmt.Sprintf("Latest %s %s, target %s.", fmtVal(g.Value), g.Unit, goalRange(g.Min, g.Max)), Action: planAction{Type: "biomarker", Ref: g.Code, Name: l.Name}})
	}
	goalCodes := map[string]bool{}
	for _, g := range off {
		goalCodes[g.Code] = true
	}
	n := 0
	for _, l := range flagged {
		if goalCodes[l.Code] || n == 2 {
			continue
		}
		n++
		tasks = append(tasks, planTask{ID: "lab-" + l.Code, Kind: "lab", Title: "Review your " + l.Name,
			Why: fmt.Sprintf("%s %s is outside its range (%s).", fmtVal(l.Value), l.Unit, l.Range), Action: planAction{Type: "biomarker", Ref: l.Code, Name: l.Name}})
	}
	slipping := []habitView{}
	for _, h := range habits {
		if h.Drop() > 0 && h.Target != nil && h.Avg7 != nil && h.Baseline != nil {
			slipping = append(slipping, h)
		}
	}
	sort.Slice(slipping, func(i, j int) bool { return slipping[i].Drop() > slipping[j].Drop() })
	for i, h := range slipping {
		if i == 2 {
			break
		}
		why := "Your 7-day average is " + withUnit(*h.Avg7, h.Unit) + ", down from your usual " + withUnit(*h.Baseline, h.Unit)
		if wellbeing.Benefit[h.Code] < 0 {
			why = "Your 7-day average is " + withUnit(*h.Avg7, h.Unit) + ", up from your usual " + withUnit(*h.Baseline, h.Unit)
		}
		if h.Projected != nil {
			why += ", heading to " + withUnit(*h.Projected, h.Unit) + " next week"
		}
		tasks = append(tasks, planTask{ID: "habit-" + h.Code, Kind: "habit", Title: "Bring your " + lowerFirst(h.Name) + " back toward " + withUnit(*h.Target, h.Unit),
			Why: why + ".", Action: planAction{Type: "log", Ref: h.Code, Name: h.Name}})
	}
	n = 0
	for _, h := range habits {
		if wellbeing.Benefit[h.Code] == 0 || h.LoggedNow || h.Days7 < 3 || n == 2 {
			continue
		}
		n++
		tasks = append(tasks, planTask{ID: "log-" + h.Code, Kind: "log", Title: "Log today’s " + lowerFirst(h.Name),
			Why: fmt.Sprintf("You logged it on %d of the last 7 days; one entry keeps your trend accurate.", h.Days7), Action: planAction{Type: "log", Ref: h.Code, Name: h.Name}})
	}
	if len(tasks) > 6 {
		tasks = tasks[:6]
	}

	httpx.WriteJSON(w, 200, map[string]any{
		"score":  wellbeing.Combine(parts),
		"parts":  parts,
		"habits": habits,
		"labs":   map[string]int{"total": len(labs), "in_range": inRange, "out_of_range": len(flagged), "unrated": unrated},
		"care":   map[string]int{"goals": len(goals), "assessed_goals": assessed, "on_target": onTarget, "dated_follow_ups": open, "overdue_follow_ups": overdue, "open_follow_ups": open},
		"tasks":  tasks,
		"basis":  "Built from your own labs, habit logs and care plan. Targets are your own best days, not population averages.",
	})
}

func goalRange(lo, hi *float64) string {
	switch {
	case lo != nil && hi != nil:
		return fmtVal(*lo) + "–" + fmtVal(*hi)
	case hi != nil:
		return "up to " + fmtVal(*hi)
	case lo != nil:
		return "at least " + fmtVal(*lo)
	}
	return ""
}

func shortName(n string) string {
	if i := strings.Index(n, " ("); i > 0 {
		return n[:i]
	}
	return n
}

func withUnit(v float64, unit string) string {
	if unit == "score" {
		return fmtVal(v) + "/5"
	}
	return fmtVal(v) + " " + unit
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return string(append([]rune{[]rune(s)[0] | 0x20}, []rune(s)[1:]...))
}
