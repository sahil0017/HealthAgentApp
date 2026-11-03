package commandcentre

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/domain/health"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// loadSeries returns each biomarker's verified readings, oldest first. Unverified values never
// feed goals, trends or reminders.
func (m *Module) loadSeries(ctx context.Context, ownerID, profileID string) (map[string]*biomarker.Series, error) {
	rows, err := m.DB.Pool.Query(ctx, `SELECT biomarker_code,name,value::float8,unit,COALESCE(observed_at,created_at::date) FROM observations WHERE profile_id=$1 AND owner_id=$2 AND biomarker_code IS NOT NULL AND verification_status='VERIFIED' ORDER BY biomarker_code,COALESCE(observed_at,created_at::date),created_at`, profileID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*biomarker.Series{}
	for rows.Next() {
		var code, name string
		var r biomarker.Reading
		if err := rows.Scan(&code, &name, &r.Value, &r.Unit, &r.On); err != nil {
			return nil, err
		}
		if out[code] == nil {
			out[code] = &biomarker.Series{Name: name}
		}
		out[code].Readings = append(out[code].Readings, r)
	}
	return out, rows.Err()
}

type GoalView struct {
	ID            string    `json:"id"`
	BiomarkerCode string    `json:"biomarker_code"`
	Title         string    `json:"title"`
	TargetMin     *float64  `json:"target_min"`
	TargetMax     *float64  `json:"target_max"`
	Unit          string    `json:"unit"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	Progress      struct {
		health.GoalEvaluation
		LatestValue *float64 `json:"latest_value"`
		LatestUnit  *string  `json:"latest_unit"`
		LatestOn    *string  `json:"latest_on"`
	} `json:"progress"`
}

const goalColumns = `id::text,biomarker_code,title,target_min::float8,target_max::float8,unit,status,created_at`

func scanGoal(row pgx.Row) (GoalView, error) {
	var g GoalView
	err := row.Scan(&g.ID, &g.BiomarkerCode, &g.Title, &g.TargetMin, &g.TargetMax, &g.Unit, &g.Status, &g.CreatedAt)
	return g, err
}

func (g *GoalView) applyProgress(all map[string]*biomarker.Series) {
	var latest, previous *biomarker.Reading
	if s := all[g.BiomarkerCode]; s != nil {
		latest, previous = biomarker.LastTwo(s.Readings)
	}
	g.Progress.GoalEvaluation = health.EvaluateGoal(health.GoalTarget{Min: g.TargetMin, Max: g.TargetMax, Unit: g.Unit}, latest, previous)
	if latest != nil {
		day := latest.On.Format("2006-01-02")
		g.Progress.LatestValue, g.Progress.LatestUnit, g.Progress.LatestOn = &latest.Value, &latest.Unit, &day
	}
}

func (m *Module) listGoals(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	goals, err := m.goalsWithProgress(r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID"), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteError(w, 500, "GOALS_FAILED", "Goals could not be loaded.", nil)
		return
	}
	httpx.WriteJSON(w, 200, goals)
}

func (m *Module) goalsWithProgress(ctx context.Context, ownerID, profileID, status string) ([]GoalView, error) {
	all, err := m.loadSeries(ctx, ownerID, profileID)
	if err != nil {
		return nil, err
	}
	rows, err := m.DB.Pool.Query(ctx, `SELECT `+goalColumns+` FROM goals WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR status=$3) ORDER BY status, created_at DESC`, profileID, ownerID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	goals := []GoalView{}
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		g.applyProgress(all)
		goals = append(goals, g)
	}
	return goals, rows.Err()
}

type goalInput struct {
	BiomarkerCode *string  `json:"biomarker_code"`
	Title         *string  `json:"title"`
	TargetMin     *float64 `json:"target_min"`
	TargetMax     *float64 `json:"target_max"`
	Unit          *string  `json:"unit"`
	Status        *string  `json:"status"`
}

func (m *Module) createGoal(w http.ResponseWriter, r *http.Request) {
	var in goalInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil || in.BiomarkerCode == nil || in.Unit == nil {
		httpx.Invalid(w, "INVALID_GOAL", "A biomarker, unit and at least one target are required.", "body")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(*in.BiomarkerCode))
	unit := strings.TrimSpace(*in.Unit)
	if in.TargetMin == nil && in.TargetMax == nil {
		httpx.Invalid(w, "INVALID_GOAL", "Set a minimum, a maximum or both.", "target_min")
		return
	}
	if in.TargetMin != nil && in.TargetMax != nil && *in.TargetMin > *in.TargetMax {
		httpx.Invalid(w, "INVALID_GOAL", "The minimum cannot exceed the maximum.", "target_min")
		return
	}
	if !httpx.ValidText(unit, 40, true) {
		httpx.Invalid(w, "INVALID_GOAL", "A unit is required.", "unit")
		return
	}
	if !m.CanStore(w, r) {
		return
	}
	var display string
	var supported []string
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT display_name,supported_units FROM biomarker_catalog WHERE code=$1`, code).Scan(&display, &supported); err != nil {
		httpx.WriteError(w, 400, "UNKNOWN_BIOMARKER", "The biomarker code is not in the catalog.", map[string]string{"biomarker_code": "unknown"})
		return
	}
	if len(supported) > 0 && !httpx.Contains(supported, unit) {
		httpx.WriteError(w, 400, "INCOMPATIBLE_UNIT", "The unit is not supported for this biomarker.", map[string]string{"unit": "unsupported"})
		return
	}
	title := display
	if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
		title = strings.TrimSpace(*in.Title)
	}
	if !httpx.ValidText(title, 200, true) {
		httpx.Invalid(w, "INVALID_GOAL", "The title is too long.", "title")
		return
	}
	profileID := chi.URLParam(r, "profileID")
	g, err := scanGoal(m.DB.Pool.QueryRow(r.Context(), `INSERT INTO goals(owner_id,profile_id,biomarker_code,title,target_min,target_max,unit) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+goalColumns, httpx.Owner(r), profileID, code, title, in.TargetMin, in.TargetMax, unit))
	if err != nil {
		httpx.WriteError(w, 500, "GOALS_FAILED", "The goal could not be saved.", nil)
		return
	}
	all, _ := m.loadSeries(r.Context(), httpx.Owner(r), profileID)
	g.applyProgress(all)
	m.audit(r, profileID, "goal.created", "goal", g.ID)
	httpx.WriteJSON(w, 201, g)
}

func (m *Module) updateGoal(w http.ResponseWriter, r *http.Request) {
	var in goalInput
	if httpx.DecodeJSON(r, 16<<10, &in) != nil || in.BiomarkerCode != nil {
		httpx.Invalid(w, "INVALID_GOAL", "The goal is not valid; the biomarker cannot be changed.", "body")
		return
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		httpx.Invalid(w, "INVALID_GOAL", "status must be active or archived.", "status")
		return
	}
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		in.Title = &trimmed
		if !httpx.ValidText(trimmed, 200, true) {
			httpx.Invalid(w, "INVALID_GOAL", "The title must be 1 to 200 characters.", "title")
			return
		}
	}
	if in.Unit != nil {
		trimmed := strings.TrimSpace(*in.Unit)
		in.Unit = &trimmed
		if !httpx.ValidText(trimmed, 40, true) {
			httpx.Invalid(w, "INVALID_GOAL", "A unit is required.", "unit")
			return
		}
	}
	g, err := scanGoal(m.DB.Pool.QueryRow(r.Context(), `UPDATE goals SET title=COALESCE($3,title),target_min=COALESCE($4,target_min),target_max=COALESCE($5,target_max),unit=COALESCE($6,unit),status=COALESCE($7,status),updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING `+goalColumns, chi.URLParam(r, "goalID"), httpx.Owner(r), in.Title, in.TargetMin, in.TargetMax, in.Unit, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, 404, "GOAL_NOT_FOUND", "The goal could not be found.", nil)
		return
	}
	if err != nil {
		httpx.Invalid(w, "INVALID_GOAL", "The targets are not valid.", "target_min")
		return
	}
	profileID := m.profileOfResource(r, "goal", g.ID)
	all, _ := m.loadSeries(r.Context(), httpx.Owner(r), profileID)
	g.applyProgress(all)
	m.audit(r, profileID, "goal.updated", "goal", g.ID)
	httpx.WriteJSON(w, 200, g)
}

func (m *Module) deleteGoal(w http.ResponseWriter, r *http.Request) {
	m.deleteOwned(w, r, "goals", chi.URLParam(r, "goalID"), "GOAL_NOT_FOUND", "goal.deleted", "goal")
}

type TimelineItem struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurred_at"`
}

var timelineKinds = []string{"document", "observation", "history", "follow_up", "goal"}

const timelineSQL = `
WITH items AS (
  SELECT 'document' AS kind,id::text AS id,file_name AS title,replace(status,'_',' ') AS detail,created_at AS at FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status NOT IN ('deleting','deleted')
  UNION ALL SELECT 'observation',id::text,name||' '||value::float8::text||' '||unit,lower(verification_status),COALESCE(observed_at::timestamptz,created_at) FROM observations WHERE profile_id=$1 AND owner_id=$2
  UNION ALL SELECT 'history',id::text,title,replace(kind,'_',' ')||' · '||status,COALESCE(started_on::timestamptz,created_at) FROM history_entries WHERE profile_id=$1 AND owner_id=$2
  UNION ALL SELECT 'follow_up',id::text,title,kind||' · '||status,COALESCE(due_on::timestamptz,created_at) FROM follow_ups WHERE profile_id=$1 AND owner_id=$2
  UNION ALL SELECT 'goal',id::text,title,status,created_at FROM goals WHERE profile_id=$1 AND owner_id=$2
)
SELECT kind,id,title,detail,at FROM items WHERE (cardinality($3::text[])=0 OR kind=ANY($3)) AND ($6::text::date IS NULL OR at >= $6::text::date) AND ($7::text::date IS NULL OR at < $7::text::date + 1) ORDER BY at DESC,id LIMIT $4 OFFSET $5`

func (m *Module) timeline(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	kinds := []string{}
	if raw := r.URL.Query().Get("kind"); raw != "" {
		for _, kind := range strings.Split(raw, ",") {
			if !httpx.Contains(timelineKinds, kind) {
				httpx.Invalid(w, "INVALID_TIMELINE_KIND", "Unsupported timeline kind.", "kind")
				return
			}
			kinds = append(kinds, kind)
		}
	}
	from, errFrom := httpx.ParseDay(httpx.OptionalQuery(r, "from"))
	to, errTo := httpx.ParseDay(httpx.OptionalQuery(r, "to"))
	if errFrom != nil || errTo != nil || (from != nil && to != nil && *to < *from) {
		httpx.Invalid(w, "INVALID_TIMELINE_RANGE", "from and to must be YYYY-MM-DD and from cannot be after to.", "from")
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), timelineSQL, chi.URLParam(r, "profileID"), httpx.Owner(r), kinds, limit, offset, from, to)
	if err != nil {
		httpx.WriteError(w, 500, "TIMELINE_FAILED", "The timeline could not be loaded.", nil)
		return
	}
	defer rows.Close()
	items := []TimelineItem{}
	for rows.Next() {
		var item TimelineItem
		if err := rows.Scan(&item.Kind, &item.ID, &item.Title, &item.Detail, &item.OccurredAt); err != nil {
			httpx.WriteError(w, 500, "TIMELINE_FAILED", "The timeline could not be loaded.", nil)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, 200, items)
}

type AttentionItem struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // action or info
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	RefType  string `json:"ref_type,omitempty"`
	RefID    string `json:"ref_id,omitempty"`
}

type Overview struct {
	Profile struct {
		ID          string   `json:"id"`
		DisplayName string   `json:"display_name"`
		Age         *int     `json:"age"`
		Sex         *string  `json:"sex"`
		BloodGroup  *string  `json:"blood_group"`
		HeightCM    *float64 `json:"height_cm"`
	} `json:"profile"`
	Summary struct {
		Documents        int `json:"documents"`
		Observations     int `json:"observations"`
		BiomarkersTraced int `json:"biomarkers_tracked"`
		ActiveGoals      int `json:"active_goals"`
		OpenFollowUps    int `json:"open_follow_ups"`
		HistoryEntries   int `json:"history_entries"`
	} `json:"summary"`
	Attention     []AttentionItem        `json:"attention"`
	RecentChanges []health.ChangeSummary `json:"recent_changes"`
	Goals         []GoalView             `json:"goals"`
	NextFollowUp  *FollowUp              `json:"next_follow_up"`
	Suggestions   []health.Suggestion    `json:"suggested_follow_ups"`
	Reminders     []FollowUp             `json:"reminders"`
	Completeness  health.Completeness    `json:"completeness"`
}

const reminderWindowDays = 7

func (m *Module) overview(w http.ResponseWriter, r *http.Request) {
	profileID, ownerID := chi.URLParam(r, "profileID"), httpx.Owner(r)
	var out Overview
	var timezone string
	var birth *time.Time
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT id::text,display_name,timezone,date_of_birth,sex,blood_group,height_cm::float8 FROM profiles WHERE id=$1 AND owner_id=$2`, profileID, ownerID).Scan(&out.Profile.ID, &out.Profile.DisplayName, &timezone, &birth, &out.Profile.Sex, &out.Profile.BloodGroup, &out.Profile.HeightCM); err != nil {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.UTC
	}
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if birth != nil {
		age := biomarker.AgeYears(*birth, today)
		out.Profile.Age = &age
	}

	if err := m.fillOverview(r.Context(), &out, ownerID, profileID, today); err != nil {
		httpx.WriteError(w, 500, "OVERVIEW_FAILED", "The overview could not be loaded.", nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}

func (m *Module) fillOverview(ctx context.Context, out *Overview, ownerID, profileID string, today time.Time) error {
	facts := health.CompletenessFacts{}
	var toReview, failed int
	if err := m.DB.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status NOT IN ('deleting','deleted')),count(*) FILTER (WHERE status IN ('review_required','manual_review_required')),count(*) FILTER (WHERE status='failed') FROM medical_documents WHERE profile_id=$1 AND owner_id=$2`, profileID, ownerID).Scan(&out.Summary.Documents, &toReview, &failed); err != nil {
		return err
	}
	var pending int
	if err := m.DB.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE verification_status='PENDING_REVIEW') FROM observations WHERE profile_id=$1 AND owner_id=$2`, profileID, ownerID).Scan(&out.Summary.Observations, &pending); err != nil {
		return err
	}
	if err := m.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM history_entries WHERE profile_id=$1 AND owner_id=$2),(SELECT count(*) FROM goals WHERE profile_id=$1 AND owner_id=$2 AND status='active'),(SELECT count(*) FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open')`, profileID, ownerID).Scan(&out.Summary.HistoryEntries, &out.Summary.ActiveGoals, &out.Summary.OpenFollowUps); err != nil {
		return err
	}
	facts.StorageConsent = m.HasConsent(ctx, profileID, "medical_record_storage") || m.HasConsent(ctx, profileID, "health_data")
	facts.ProcessingConsent = m.HasConsent(ctx, profileID, "document_processing") || m.HasConsent(ctx, profileID, "ai_processing")
	facts.ProfileDetails = out.Profile.Age != nil && out.Profile.Sex != nil
	facts.Documents, facts.DocumentsToReview, facts.ObservationsPending = out.Summary.Documents, toReview, pending
	facts.HistoryEntries, facts.ActiveGoals = out.Summary.HistoryEntries, out.Summary.ActiveGoals
	out.Completeness = health.AssessCompleteness(facts)

	attention := []AttentionItem{}
	if !facts.StorageConsent || !facts.ProcessingConsent {
		attention = append(attention, AttentionItem{Kind: "consent", Severity: "action", Title: "Choose your data permissions", Detail: "Report storage and processing are needed before you can add records."})
	}
	if toReview > 0 {
		attention = append(attention, AttentionItem{Kind: "review", Severity: "action", Title: httpx.Plural(toReview, "report is", "reports are") + " waiting for your review", Detail: "Check the extracted values before they join your record."})
	}
	if failed > 0 {
		attention = append(attention, AttentionItem{Kind: "failed_document", Severity: "action", Title: httpx.Plural(failed, "report", "reports") + " could not be read", Detail: "Retry the upload or add the values by hand."})
	}
	if pending > 0 {
		attention = append(attention, AttentionItem{Kind: "unverified", Severity: "info", Title: httpx.Plural(pending, "value is", "values are") + " not yet verified", Detail: "Unverified values are left out of trends and goals."})
	}

	followUps, err := m.openFollowUps(ctx, ownerID, profileID)
	if err != nil {
		return err
	}
	openTitles := make([]string, len(followUps))
	for i, f := range followUps {
		openTitles[i] = f.Title
	}
	candidates := []health.Suggestion{}
	out.Reminders = []FollowUp{}
	horizon := today.AddDate(0, 0, reminderWindowDays)
	for i := range followUps {
		f := followUps[i]
		if f.DueOn == nil {
			continue
		}
		due, _ := time.Parse("2006-01-02", *f.DueOn)
		if due.Before(today) {
			attention = append(attention, AttentionItem{Kind: "overdue_follow_up", Severity: "action", Title: f.Title + " is overdue", Detail: "Due " + *f.DueOn, RefType: "follow_up", RefID: f.ID})
		}
		if !due.After(horizon) {
			out.Reminders = append(out.Reminders, f)
		}
		if out.NextFollowUp == nil && !due.Before(today) {
			out.NextFollowUp = &followUps[i]
		}
	}

	all, err := m.loadSeries(ctx, ownerID, profileID)
	if err != nil {
		return err
	}
	out.Summary.BiomarkersTraced = len(all)
	changes := []health.ChangeSummary{}
	codes := make([]string, 0, len(all))
	for code := range all {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		s := all[code]
		if c, ok := health.SummarizeChange(code, s.Name, s.Readings); ok {
			changes = append(changes, c)
		}
		dates := make([]time.Time, len(s.Readings))
		for i, rd := range s.Readings {
			dates[i] = rd.On
		}
		if cadence, ok := biomarker.MeasurementCadence(dates); ok {
			last := s.Readings[len(s.Readings)-1].On
			if biomarker.OverdueForMeasurement(last, today, cadence) {
				attention = append(attention, AttentionItem{Kind: "measurement_gap", Severity: "info", Title: s.Name + " has not been measured lately", Detail: "You usually measure it about every " + httpx.Plural(int(cadence.Hours()/24+0.5), "day", "days") + "; the last reading was " + last.Format("2006-01-02") + ".", RefType: "biomarker", RefID: code})
				candidates = append(candidates, health.Suggestion{Kind: "investigation", Title: "Repeat " + s.Name, Reason: "Your usual gap is about " + httpx.Plural(int(cadence.Hours()/24+0.5), "day", "days") + " and it has been longer.", RefType: "biomarker", RefID: code})
			}
		}
	}
	attention = append(attention, m.outOfRangeAttention(ctx, ownerID, profileID, today)...)
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].ObservedOn > changes[j].ObservedOn })
	if len(changes) > 8 {
		changes = changes[:8]
	}
	out.RecentChanges = changes

	goals, err := m.goalsWithProgress(ctx, ownerID, profileID, "active")
	if err != nil {
		return err
	}
	out.Goals = goals
	for _, g := range goals {
		if g.Progress.LatestValue == nil {
			attention = append(attention, AttentionItem{Kind: "goal_without_data", Severity: "info", Title: g.Title + " has no verified readings yet", Detail: "Add or verify a reading to see progress toward this goal.", RefType: "goal", RefID: g.ID})
		}
		if g.Progress.Status == "below_target" || g.Progress.Status == "above_target" {
			candidates = append(candidates, health.Suggestion{Kind: "investigation", Title: "Recheck " + g.Title, Reason: "Your goal is " + strings.ReplaceAll(g.Progress.Status, "_", " ") + ".", RefType: "goal", RefID: g.ID})
			attention = append(attention, AttentionItem{Kind: "goal_off_target", Severity: "info", Title: g.Title + " is " + strings.ReplaceAll(g.Progress.Status, "_", " "), Detail: "Trend: " + g.Progress.Trend + ".", RefType: "goal", RefID: g.ID})
		}
	}
	sort.SliceStable(attention, func(i, j int) bool { return attention[i].Severity == "action" && attention[j].Severity != "action" })
	out.Attention = attention
	out.Suggestions = health.WithoutExisting(candidates, openTitles)
	return nil
}

func (m *Module) openFollowUps(ctx context.Context, ownerID, profileID string) ([]FollowUp, error) {
	rows, err := m.DB.Pool.Query(ctx, `SELECT `+followUpColumns+` FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open' ORDER BY due_on NULLS LAST,created_at`, profileID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []FollowUp{}
	for rows.Next() {
		f, err := scanFollowUp(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, f)
	}
	return items, rows.Err()
}

// outOfRangeAttention surfaces the latest verified reading of each biomarker that sits outside its
// reference range. It is an observation to raise with a clinician, never a diagnosis.
func (m *Module) outOfRangeAttention(ctx context.Context, ownerID, profileID string, now time.Time) []AttentionItem {
	byCode, names, err := observations.New(m.DB).LoadObservationRows(ctx, ownerID, profileID, nil)
	if err != nil {
		return nil
	}
	sex, age := observations.New(m.DB).ProfileDemographics(ctx, ownerID, profileID, now)
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	items := []AttentionItem{}
	for _, code := range codes {
		known, learned, err := observations.New(m.DB).LoadKnowledge(ctx, code)
		if err != nil {
			continue
		}
		t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		if len(t.Points) == 0 {
			continue
		}
		latest := t.Points[len(t.Points)-1]
		if latest.Flag != "above_range" && latest.Flag != "below_range" {
			continue
		}
		side := "above"
		if latest.Flag == "below_range" {
			side = "below"
		}
		items = append(items, AttentionItem{Kind: "out_of_range", Severity: "info", Title: t.DisplayName + " is " + side + " its reference range", Detail: "Your latest verified reading was " + latest.ObservedAt[:min(10, len(latest.ObservedAt))] + ". Consider raising it with your clinician.", RefType: "biomarker", RefID: code})
	}
	return items
}
