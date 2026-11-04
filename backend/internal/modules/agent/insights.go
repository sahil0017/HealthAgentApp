package agent

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

var permissionDefaults = []struct {
	Key     string
	Label   string
	Default bool
}{
	{"read_labs", "Read my lab results", true},
	{"read_devices", "Read my device data", true},
	{"read_lifestyle", "Read my lifestyle and symptom log", true},
	{"propose_actions", "Suggest follow-ups for me to approve", false},
}

func (m *Module) notFound(w http.ResponseWriter) {
	httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
}

// granted reports whether the user has allowed the agent a capability, falling back to its default.
func (m *Module) granted(ctx context.Context, profileID, permission string) bool {
	var g bool
	if err := m.DB.Pool.QueryRow(ctx, `SELECT granted FROM agent_permissions WHERE profile_id=$1 AND permission=$2`, profileID, permission).Scan(&g); err == nil {
		return g
	}
	for _, d := range permissionDefaults {
		if d.Key == permission {
			return d.Default
		}
	}
	return false
}

func (m *Module) listPermissions(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	profileID := chi.URLParam(r, "profileID")
	type perm struct {
		Permission string `json:"permission"`
		Label      string `json:"label"`
		Granted    bool   `json:"granted"`
	}
	out := []perm{}
	for _, d := range permissionDefaults {
		out = append(out, perm{d.Key, d.Label, m.granted(r.Context(), profileID, d.Key)})
	}
	httpx.WriteJSON(w, 200, map[string]any{"permissions": out})
}

func (m *Module) setPermission(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	profileID, key := chi.URLParam(r, "profileID"), chi.URLParam(r, "permission")
	known := slices.ContainsFunc(permissionDefaults, func(d struct {
		Key     string
		Label   string
		Default bool
	}) bool {
		return d.Key == key
	})
	var in struct {
		Granted *bool `json:"granted"`
	}
	if !known || httpx.DecodeJSON(r, 1<<10, &in) != nil || in.Granted == nil {
		httpx.WriteError(w, 400, "INVALID_PERMISSION", "A known permission and a granted value are required.", nil)
		return
	}
	_, err := m.DB.Pool.Exec(r.Context(), `INSERT INTO agent_permissions(profile_id,owner_id,permission,granted) VALUES($1,$2,$3,$4)
		ON CONFLICT(profile_id,permission) DO UPDATE SET granted=EXCLUDED.granted,updated_at=now()`, profileID, httpx.Owner(r), key, *in.Granted)
	if err != nil {
		httpx.WriteError(w, 500, "PERMISSION_WRITE_FAILED", "The permission could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"permission": key, "granted": *in.Granted})
}

type flaggedLab struct {
	Code          string   `json:"biomarker_code"`
	Name          string   `json:"name"`
	Value         float64  `json:"value"`
	Unit          string   `json:"unit"`
	Flag          string   `json:"flag"`
	ObservedAt    string   `json:"observed_at"`
	ObservationID string   `json:"observation_id"`
	Previous      *float64 `json:"previous_value,omitempty"`
}

func (l flaggedLab) describe() string {
	dir := "above"
	if l.Flag == "below_range" {
		dir = "below"
	}
	return fmt.Sprintf("%s is %s its reference range (%v %s)", l.Name, dir, l.Value, l.Unit)
}

// flaggedLabs returns the latest verified reading of each biomarker that sits outside its range.
func (m *Module) flaggedLabs(ctx context.Context, owner, profileID string) ([]flaggedLab, error) {
	repo := observations.New(m.DB)
	byCode, names, err := repo.LoadObservationRows(ctx, owner, profileID, nil)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sex, age := repo.ProfileDemographics(ctx, owner, profileID, now)
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	out := []flaggedLab{}
	for _, code := range codes {
		known, learned, err := repo.LoadKnowledge(ctx, code)
		if err != nil {
			return nil, err
		}
		t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		if len(t.Points) == 0 {
			continue
		}
		p := t.Points[len(t.Points)-1]
		if p.Flag == "below_range" || p.Flag == "above_range" {
			l := flaggedLab{Code: code, Name: t.DisplayName, Value: p.Value, Unit: t.CanonicalUnit, Flag: p.Flag, ObservedAt: p.ObservedAt, ObservationID: p.ObservationID}
			if n := len(t.Points); n > 1 {
				prev := t.Points[n-2].Value
				l.Previous = &prev
			}
			out = append(out, l)
		}
	}
	return out, nil
}

type line struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

type section struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items []line `json:"items"`
}

func (m *Module) briefing(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	out := []section{}

	if m.granted(ctx, profileID, "read_labs") {
		labs, err := m.flaggedLabs(ctx, owner, profileID)
		if err != nil {
			httpx.WriteError(w, 500, "BRIEFING_UNAVAILABLE", "The briefing could not be prepared.", nil)
			return
		}
		s := section{Key: "labs", Title: "Lab results to look at", Items: []line{}}
		for _, l := range labs {
			s.Items = append(s.Items, line{Text: l.describe(), Tone: "attention"})
		}
		out = append(out, s)
	}

	fu := section{Key: "follow_ups", Title: "Follow-ups", Items: []line{}}
	rows, err := m.DB.Pool.Query(ctx, `SELECT title, due_on::text, due_on IS NOT NULL AND due_on < current_date FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open' ORDER BY due_on NULLS LAST LIMIT 20`, profileID, owner)
	if err == nil {
		for rows.Next() {
			var title string
			var due *string
			var overdue bool
			if rows.Scan(&title, &due, &overdue) == nil {
				l := line{Text: title}
				if due != nil {
					l.Text += " (due " + *due + ")"
				}
				if overdue {
					l.Tone = "attention"
				}
				fu.Items = append(fu.Items, l)
			}
		}
		rows.Close()
	}
	out = append(out, fu)

	if m.granted(ctx, profileID, "read_lifestyle") {
		sy := section{Key: "symptoms", Title: "Symptoms still active", Items: []line{}}
		rows, err := m.DB.Pool.Query(ctx, `SELECT name, severity FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND ended_at IS NULL ORDER BY started_at DESC LIMIT 20`, profileID, owner)
		if err == nil {
			for rows.Next() {
				var name string
				var sev int
				if rows.Scan(&name, &sev) == nil {
					sy.Items = append(sy.Items, line{Text: fmt.Sprintf("%s (severity %d/10)", name, sev)})
				}
			}
			rows.Close()
		}
		out = append(out, sy)
	}

	var review int
	_ = m.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status IN ('review_required','manual_review_required')`, profileID, owner).Scan(&review)
	docs := section{Key: "documents", Title: "Reports", Items: []line{}}
	if review > 0 {
		docs.Items = append(docs.Items, line{Text: fmt.Sprintf("%d report(s) are waiting for your review", review), Tone: "attention"})
	}
	out = append(out, docs)

	var pending int
	_ = m.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM agent_actions WHERE profile_id=$1 AND status='proposed'`, profileID).Scan(&pending)
	httpx.WriteJSON(w, 200, map[string]any{
		"generated_at":      time.Now().UTC(),
		"sections":          out,
		"pending_approvals": pending,
		"note":              "This summary restates your own records. It is not a diagnosis or medical advice.",
	})
}

func (m *Module) visitPrep(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	labs := []flaggedLab{}
	if m.granted(ctx, profileID, "read_labs") {
		var err error
		if labs, err = m.flaggedLabs(ctx, owner, profileID); err != nil {
			httpx.WriteError(w, 500, "VISIT_PREP_UNAVAILABLE", "The summary could not be prepared.", nil)
			return
		}
	}
	collect := func(query string, args ...any) []string {
		out := []string{}
		rows, err := m.DB.Pool.Query(ctx, query, args...)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				out = append(out, s)
			}
		}
		return out
	}
	symptoms := []string{}
	if m.granted(ctx, profileID, "read_lifestyle") {
		symptoms = collect(`SELECT name||' (severity '||severity||'/10, since '||started_at::date||')' FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND ended_at IS NULL ORDER BY started_at DESC LIMIT 20`, profileID, owner)
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"generated_at":    time.Now().UTC(),
		"labs_to_discuss": labs,
		"active_symptoms": symptoms,
		"medications":     collect(`SELECT title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND kind='medication' AND status='active' ORDER BY title`, profileID, owner),
		"conditions":      collect(`SELECT title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND kind IN ('condition','allergy') AND status='active' ORDER BY title`, profileID, owner),
		"open_follow_ups": collect(`SELECT title FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open' ORDER BY due_on NULLS LAST LIMIT 20`, profileID, owner),
		"goals":           collect(`SELECT title FROM goals WHERE profile_id=$1 AND owner_id=$2 AND status='active' ORDER BY created_at DESC LIMIT 20`, profileID, owner),
		"questions":       visitQuestions(labs),
		"note":            "A summary of your own records to bring to an appointment. It is not a diagnosis.",
	})
}

// visitQuestions are neutral prompts built from the user's own out-of-range results. They are not advice.
func visitQuestions(labs []flaggedLab) []string {
	out := []string{}
	for _, l := range labs {
		out = append(out, "What could explain my "+l.Name+" being outside its range?")
	}
	if len(labs) > 0 {
		out = append(out, "When should I repeat these tests?")
	}
	out = append(out, "Is there anything in my records I should follow up on?")
	return out
}
