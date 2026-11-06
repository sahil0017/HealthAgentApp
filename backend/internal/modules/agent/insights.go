package agent

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
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
	{"cloud_model", "Let the agent use a cloud AI model (sends only the lab values it reads to answer your question)", false},
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
		s := section{Key: "labs", Title: "Results needing attention", Items: []line{}}
		for _, l := range labs {
			s.Items = append(s.Items, line{Text: l.describe(), Tone: "attention"})
		}
		out = append(out, s)
	}

	fu := section{Key: "follow_ups", Title: "Pending follow-ups", Items: []line{}}
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
		sy := section{Key: "symptoms", Title: "Active symptoms", Items: []line{}}
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

	rx := section{Key: "medicines", Title: "Medication schedule", Items: []line{}}
	for _, sl := range m.todaysDoses(ctx, owner, profileID, r.URL.Query().Get("day")) {
		rx.Items = append(rx.Items, line{Text: sl})
	}
	out = append(out, rx)

	var review int
	_ = m.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status IN ('review_required','manual_review_required')`, profileID, owner).Scan(&review)
	docs := section{Key: "documents", Title: "Recent reports", Items: []line{}}
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
	// Saved prescriptions are the source of truth for current medicines; history entries add the rest.
	meds := collect(`SELECT trim(i.name||' '||i.strength||CASE WHEN i.frequency_text<>'' THEN ' — '||i.frequency_text ELSE '' END)
		FROM prescription_items i JOIN prescriptions p ON p.id=i.prescription_id
		WHERE i.profile_id=$1 AND p.owner_id=$2 AND i.active ORDER BY i.name`, profileID, owner)
	have := map[string]bool{}
	for _, s := range meds {
		have[strings.ToLower(strings.Fields(s + " ")[0])] = true
	}
	for _, s := range collect(`SELECT title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND source='user' AND kind='medication' AND status='active' ORDER BY title`, profileID, owner) {
		if f := strings.Fields(s); len(f) == 0 || !have[strings.ToLower(f[0])] {
			meds = append(meds, s)
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"generated_at":    time.Now().UTC(),
		"labs_to_discuss": labs,
		"active_symptoms": symptoms,
		"medications":     meds,
		"conditions":      collect(`SELECT title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND source='user' AND kind IN ('condition','allergy') AND status='active' ORDER BY title`, profileID, owner),
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

// suggestions are starter questions derived only from the user's own record; an empty record yields none.
func (m *Module) suggestions(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	out := []string{}
	add := func(q string) {
		if len(out) < 6 {
			out = append(out, q)
		}
	}
	if m.granted(ctx, profileID, "read_labs") {
		labs, err := m.flaggedLabs(ctx, owner, profileID)
		if err != nil {
			httpx.WriteError(w, 500, "SUGGESTIONS_UNAVAILABLE", "Suggestions could not be prepared.", nil)
			return
		}
		for i, l := range labs {
			if i == 3 {
				break
			}
			add("What could explain my " + l.Name + " being outside its range?")
		}
		if len(labs) > 0 {
			add("What should I ask my doctor about these results?")
		} else {
			var name string
			if m.DB.Pool.QueryRow(ctx, `SELECT name FROM observations WHERE profile_id=$1 AND owner_id=$2 AND verification_status='VERIFIED' ORDER BY observed_at DESC LIMIT 1`, profileID, owner).Scan(&name) == nil {
				add("What was my latest " + name + "?")
			}
		}
	}
	if m.granted(ctx, profileID, "read_lifestyle") {
		var sym string
		if m.DB.Pool.QueryRow(ctx, `SELECT name FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, profileID, owner).Scan(&sym) == nil {
			add("What might be related to my " + strings.ToLower(sym) + "?")
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"suggestions": out})
}

// todaysDoses lists, per time of day, the active scheduled medicines that have not been marked taken.
func (m *Module) todaysDoses(ctx context.Context, owner, profileID, day string) []string {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		day = time.Now().UTC().Format("2006-01-02")
	}
	rows, err := m.DB.Pool.Query(ctx, `SELECT s.slot, i.name, i.strength, EXISTS(SELECT 1 FROM dose_logs d WHERE d.item_id=i.id AND d.day=$3::date AND d.slot=s.slot)
		FROM prescription_items i JOIN prescriptions p ON p.id=i.prescription_id, unnest(i.slots) AS s(slot)
		WHERE i.profile_id=$1 AND p.owner_id=$2 AND i.active AND NOT i.as_needed AND (i.ends_on IS NULL OR i.ends_on >= $3::date)
		ORDER BY array_position(ARRAY['morning','afternoon','evening','night'], s.slot), i.name`, profileID, owner, day)
	if err != nil {
		return nil
	}
	defer rows.Close()
	pending, done := map[string][]string{}, 0
	order := []string{}
	for rows.Next() {
		var slot, name, strength string
		var taken bool
		if rows.Scan(&slot, &name, &strength, &taken) != nil {
			continue
		}
		if taken {
			done++
			continue
		}
		if _, ok := pending[slot]; !ok {
			order = append(order, slot)
		}
		pending[slot] = append(pending[slot], strings.TrimSpace(name+" "+strength))
	}
	out := []string{}
	for _, slot := range order {
		out = append(out, strings.ToUpper(slot[:1])+slot[1:]+": "+strings.Join(pending[slot], ", "))
	}
	if done > 0 {
		out = append(out, fmt.Sprintf("%d dose(s) already taken today", done))
	}
	return out
}

// carePlanFacts is plain text for the agent: open follow-ups, active symptoms and reports waiting for review.
func (m *Module) carePlanFacts(ctx context.Context, owner, profileID string) string {
	parts := []string{}
	if rows, err := m.DB.Pool.Query(ctx, `SELECT title, COALESCE(due_on::text,''), due_on IS NOT NULL AND due_on < current_date FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open' ORDER BY due_on NULLS LAST LIMIT 15`, profileID, owner); err == nil {
		for rows.Next() {
			var title, due string
			var overdue bool
			if rows.Scan(&title, &due, &overdue) == nil {
				t := "Follow-up: " + title
				if due != "" {
					t += " (due " + due
					if overdue {
						t += ", overdue"
					}
					t += ")"
				}
				parts = append(parts, t)
			}
		}
		rows.Close()
	}
	if m.granted(ctx, profileID, "read_lifestyle") {
		if rows, err := m.DB.Pool.Query(ctx, `SELECT name, severity FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND ended_at IS NULL ORDER BY started_at DESC LIMIT 10`, profileID, owner); err == nil {
			for rows.Next() {
				var name string
				var sev int
				if rows.Scan(&name, &sev) == nil {
					parts = append(parts, fmt.Sprintf("Active symptom: %s (severity %d/10)", name, sev))
				}
			}
			rows.Close()
		}
	}
	var review int
	_ = m.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM medical_documents WHERE profile_id=$1 AND owner_id=$2 AND status IN ('review_required','manual_review_required')`, profileID, owner).Scan(&review)
	if review > 0 {
		parts = append(parts, fmt.Sprintf("%d report(s) are waiting for the person to review.", review))
	}
	if len(parts) == 0 {
		return "No open follow-ups, active symptoms or reports waiting."
	}
	return strings.Join(parts, "\n")
}

// briefingSummary is the agent's short written briefing. It needs the cloud model permission and falls back
// to nothing: the structured briefing above always stands on its own.
func (m *Module) briefingSummary(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	if !m.HasConsent(ctx, profileID, "ai_processing") || !m.granted(ctx, profileID, "read_labs") || !m.granted(ctx, profileID, "cloud_model") {
		httpx.WriteJSON(w, 200, map[string]any{"summary": "", "source": "none"})
		return
	}
	l := &labs{m: m, ctx: ctx, owner: owner, profile: profileID, repo: observations.New(m.DB), touched: map[string]bool{}}
	if err := l.load(); err != nil {
		httpx.WriteJSON(w, 200, map[string]any{"summary": "", "source": "none"})
		return
	}
	in := chatRequest{Message: "Write my briefing for today.", Mode: "briefing"}
	answer, model, steps, ok := m.copilotStep(l, in)
	if !ok {
		httpx.WriteJSON(w, 200, map[string]any{"summary": "", "source": "none"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"summary": answer, "source": "copilot", "model": model, "steps": steps, "disclaimer": disclaimer})
}
