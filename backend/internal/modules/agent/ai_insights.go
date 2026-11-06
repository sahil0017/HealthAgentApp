package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

type insightItem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Tone   string `json:"tone"`
}

type insightSet struct {
	Headline string        `json:"headline"`
	Items    []insightItem `json:"items"`
	// Source is "ai" when the model wrote it and "records" when it is built directly from the person's data.
	Source string `json:"source"`
	Model  string `json:"model,omitempty"`
	// AIAvailable tells the app whether turning on AI would give a richer answer.
	AIAvailable bool `json:"ai_available"`
}

type cachedInsight struct {
	at  time.Time
	val insightSet
}

var (
	insightCache   sync.Map
	insightTTL     = 6 * time.Hour
	insightKinds   = map[string]bool{"research": true, "lifestyle": true, "history": true, "labs": true, "visit": true}
	insightTimeout = 90 * time.Second
)

// aiAllowed requires the same two switches as the briefing: AI-processing consent and the cloud model permission.
func (m *Module) aiAllowed(ctx context.Context, profileID string) bool {
	return m.HasConsent(ctx, profileID, "ai_processing") && m.granted(ctx, profileID, "read_labs") && m.granted(ctx, profileID, "cloud_model")
}

func callInsights(ctx context.Context, kind, facts string) (insightSet, error) {
	var out insightSet
	body, _ := json.Marshal(map[string]string{"kind": kind, "facts": facts})
	cctx, cancel := context.WithTimeout(ctx, insightTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, embedding.ServiceURL()+"/v1/insights", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, io.ErrUnexpectedEOF
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	out.Source = "ai"
	return out, err
}

// insight returns the model's reading of the facts, cached for a few hours so screens open instantly.
// When the model is off or unreachable it returns the fallback built from the person's own records.
func (m *Module) insight(ctx context.Context, profileID, kind, facts string, fallback insightSet) insightSet {
	fallback.Source, fallback.AIAvailable = "records", false
	if !m.aiAllowed(ctx, profileID) {
		fallback.AIAvailable = true
		return fallback
	}
	sum := sha256.Sum256([]byte(profileID + "|" + kind + "|" + facts))
	key := hex.EncodeToString(sum[:])
	if v, ok := insightCache.Load(key); ok {
		if c := v.(cachedInsight); time.Since(c.at) < insightTTL {
			return c.val
		}
	}
	res, err := callInsights(ctx, kind, facts)
	if err != nil || len(res.Items) == 0 {
		return fallback
	}
	res.AIAvailable = false
	insightCache.Store(key, cachedInsight{at: time.Now(), val: res})
	return res
}

type labLine struct {
	Code, Name, Unit, Flag, Range, On, DocID string
	Value                                    float64
	Previous                                 *float64
}

func (l labLine) text() string {
	s := fmt.Sprintf("%s %g %s (%s", l.Name, l.Value, l.Unit, strings.ReplaceAll(l.Flag, "_", " "))
	if l.Range != "" {
		s += ", range " + l.Range
	}
	s += ")"
	if l.Previous != nil {
		s += fmt.Sprintf(", previously %g", *l.Previous)
	}
	return s
}

func rangeText(r *biomarker.RangeInfo) string {
	if r == nil {
		return ""
	}
	switch {
	case r.Low != nil && r.High != nil:
		return fmt.Sprintf("%g-%g", *r.Low, *r.High)
	case r.High != nil:
		return fmt.Sprintf("up to %g", *r.High)
	case r.Low != nil:
		return fmt.Sprintf("at least %g", *r.Low)
	}
	return ""
}

// labLines returns the latest result of every biomarker, or, with docID set, the results that report contributed.
func (m *Module) labLines(ctx context.Context, owner, profileID, docID string) ([]labLine, error) {
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
	out := []labLine{}
	for _, code := range codes {
		known, learned, err := repo.LoadKnowledge(ctx, code)
		if err != nil {
			return nil, err
		}
		t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		idx := len(t.Points) - 1
		if docID != "" {
			idx = -1
			for i, p := range t.Points {
				if p.DocumentID == docID {
					idx = i
				}
			}
		}
		if idx < 0 {
			continue
		}
		p := t.Points[idx]
		l := labLine{Code: code, Name: t.DisplayName, Unit: t.CanonicalUnit, Flag: p.Flag, Range: rangeText(p.Range), On: p.ObservedAt, DocID: p.DocumentID, Value: p.Value}
		if idx > 0 {
			prev := t.Points[idx-1].Value
			l.Previous = &prev
		}
		out = append(out, l)
	}
	return out, nil
}

func isOut(flag string) bool { return flag == "below_range" || flag == "above_range" }

func labFacts(ls []labLine, demo string) string {
	var b strings.Builder
	if demo != "" {
		b.WriteString(demo + "\n")
	}
	for _, l := range ls {
		b.WriteString("- " + l.text() + "\n")
	}
	return b.String()
}

func (m *Module) demographics(ctx context.Context, owner, profileID string) string {
	sex, age := observations.New(m.DB).ProfileDemographics(ctx, owner, profileID, time.Now().UTC())
	parts := []string{}
	if age != nil {
		parts = append(parts, fmt.Sprintf("age %d", *age))
	}
	if sex != nil && *sex != "" {
		parts = append(parts, "sex "+*sex)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Person: " + strings.Join(parts, ", ")
}

func (m *Module) activeSymptoms(ctx context.Context, owner, profileID string) []string {
	out := []string{}
	rows, err := m.DB.Pool.Query(ctx, `SELECT name, severity FROM symptom_entries WHERE profile_id=$1 AND owner_id=$2 AND ended_at IS NULL ORDER BY started_at DESC LIMIT 12`, profileID, owner)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		var s int
		if rows.Scan(&n, &s) == nil {
			out = append(out, fmt.Sprintf("%s (severity %d/10)", n, s))
		}
	}
	return out
}

func (m *Module) historyTitles(ctx context.Context, owner, profileID string) []string {
	out := []string{}
	rows, err := m.DB.Pool.Query(ctx, `SELECT kind||': '||title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 AND source='user' AND status='active' ORDER BY created_at DESC LIMIT 20`, profileID, owner)
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

func (m *Module) lifestyleFacts(ctx context.Context, owner, profileID string) (string, int) {
	rows, err := m.DB.Pool.Query(ctx, `SELECT category, round(avg(value)::numeric,1)::float8, min(unit), count(DISTINCT logged_on), round(avg(value) FILTER (WHERE logged_on >= current_date-6)::numeric,1)::float8
		FROM lifestyle_logs WHERE profile_id=$1 AND owner_id=$2 AND logged_on >= current_date-29 GROUP BY category ORDER BY category`, profileID, owner)
	if err != nil {
		return "", 0
	}
	defer rows.Close()
	var b strings.Builder
	days := 0
	for rows.Next() {
		var cat, unit string
		var avg float64
		var n int
		var wk *float64
		if rows.Scan(&cat, &avg, &unit, &n, &wk) != nil {
			continue
		}
		days = max(days, n)
		line := fmt.Sprintf("- %s: 30-day average %g %s over %d logged days", cat, avg, unit, n)
		if wk != nil {
			line += fmt.Sprintf("; last 7 days average %g", *wk)
		}
		b.WriteString(line + "\n")
	}
	return b.String(), days
}

func (m *Module) insights(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID, kind := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "kind")
	if !insightKinds[kind] {
		httpx.Invalid(w, "INVALID_INSIGHT", "Unknown insight.", "kind")
		return
	}
	demo := m.demographics(ctx, owner, profileID)
	var facts string
	var fallback insightSet
	switch kind {
	case "lifestyle":
		life, days := m.lifestyleFacts(ctx, owner, profileID)
		sym := m.activeSymptoms(ctx, owner, profileID)
		if days == 0 && len(sym) == 0 {
			httpx.WriteJSON(w, 200, insightSet{Headline: "Log a few check-ins and your coach starts here.", Items: []insightItem{}, Source: "records"})
			return
		}
		facts = demo + "\nCheck-ins:\n" + life + "Active symptoms: " + strings.Join(sym, "; ") + "\n"
		if ls, err := m.labLines(ctx, owner, profileID, ""); err == nil {
			facts += "Out-of-range results:\n"
			for _, l := range ls {
				if isOut(l.Flag) {
					facts += "- " + l.text() + "\n"
				}
			}
		}
		fallback = insightSet{Headline: fmt.Sprintf("You logged check-ins on %d of the last 30 days.", days), Items: []insightItem{}}
		for _, s := range strings.Split(strings.TrimSpace(life), "\n") {
			if s != "" {
				fallback.Items = append(fallback.Items, insightItem{Title: strings.TrimPrefix(strings.SplitN(s, ":", 2)[0], "- "), Detail: strings.TrimSpace(strings.SplitN(s, ":", 2)[1]), Tone: "info"})
			}
		}
	default:
		ls, err := m.labLines(ctx, owner, profileID, "")
		if err != nil {
			httpx.WriteError(w, 500, "INSIGHTS_UNAVAILABLE", "Insights could not be prepared.", nil)
			return
		}
		if len(ls) == 0 {
			httpx.WriteJSON(w, 200, insightSet{Headline: "Add a lab report and your insights appear here.", Items: []insightItem{}, Source: "records"})
			return
		}
		facts = labFacts(ls, demo)
		if h := m.historyTitles(ctx, owner, profileID); len(h) > 0 {
			facts += "Known history: " + strings.Join(h, "; ") + "\n"
		}
		if s := m.activeSymptoms(ctx, owner, profileID); len(s) > 0 {
			facts += "Active symptoms: " + strings.Join(s, "; ") + "\n"
		}
		fallback = labFallback(ls)
		if kind == "visit" {
			facts += m.visitFacts(ctx, owner, profileID)
			fallback = visitFallback(ls)
		}
		if kind == "research" {
			fallback.Items = nil
			for _, l := range ls {
				if isOut(l.Flag) && len(fallback.Items) < 6 {
					fallback.Items = append(fallback.Items, insightItem{Title: "What could explain my " + l.Name + " being outside its range?", Detail: l.text(), Tone: "act"})
				}
			}
			fallback.Headline = "Questions built from your own results"
		}
	}
	httpx.WriteJSON(w, 200, m.insight(ctx, profileID, kind, facts, fallback))
}

// labFallback is the summary built straight from the records, without a model.
func labFallback(ls []labLine) insightSet {
	out := insightSet{Items: []insightItem{}}
	inRange, unrated := 0, 0
	for _, l := range ls {
		if isOut(l.Flag) {
			dir := "above"
			if l.Flag == "below_range" {
				dir = "below"
			}
			out.Items = append(out.Items, insightItem{Title: l.Name + " is " + dir + " range", Detail: l.text(), Tone: "act"})
		} else if l.Flag == "within_range" || l.Flag == "normal" {
			inRange++
		} else {
			unrated++
		}
	}
	rated := len(ls) - unrated
	if rated == 0 {
		out.Headline = "No results have a reference range for comparison."
	} else {
		out.Headline = fmt.Sprintf("%d of %d results with a reference range are in range.", inRange, rated)
	}
	if unrated > 0 {
		out.Headline += fmt.Sprintf(" %d without a range.", unrated)
	}
	if inRange > 0 && len(out.Items) < 8 {
		out.Items = append(out.Items, insightItem{Title: fmt.Sprintf("%d results are in range", inRange), Detail: "These sit inside their reference ranges.", Tone: "good"})
	}
	return out
}

// documentInsight summarises one upload: what it shows, where the weak spots are and what to do next.
func (m *Module) documentInsight(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID, docID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "documentID")
	ls, err := m.labLines(ctx, owner, profileID, docID)
	if err != nil {
		httpx.WriteError(w, 500, "INSIGHTS_UNAVAILABLE", "Insights could not be prepared.", nil)
		return
	}
	if len(ls) == 0 {
		httpx.WriteJSON(w, 200, insightSet{Headline: "No approved results from this report yet.", Items: []insightItem{}, Source: "records"})
		return
	}
	facts := labFacts(ls, m.demographics(ctx, owner, profileID))
	httpx.WriteJSON(w, 200, m.insight(ctx, profileID, "upload", facts, labFallback(ls)))
}

// uploadStats gives every upload a one-line reading (results, how many are out of range) so many reports stay scannable.
func (m *Module) uploadStats(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID")
	// Count every point per document straight from the trends.
	type stat struct {
		Results int      `json:"results"`
		Out     int      `json:"out_of_range"`
		Names   []string `json:"flagged"`
	}
	stats := map[string]*stat{}
	repo := observations.New(m.DB)
	byCode, names, err := repo.LoadObservationRows(ctx, owner, profileID, nil)
	if err != nil {
		httpx.WriteError(w, 500, "INSIGHTS_UNAVAILABLE", "Insights could not be prepared.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := repo.ProfileDemographics(ctx, owner, profileID, now)
	for code, rows := range byCode {
		known, learned, err := repo.LoadKnowledge(ctx, code)
		if err != nil {
			continue
		}
		t := biomarker.BuildTrend(code, names[code], rows, false, known, learned, sex, age, now)
		for _, p := range t.Points {
			if p.DocumentID == "" {
				continue
			}
			s := stats[p.DocumentID]
			if s == nil {
				s = &stat{Names: []string{}}
				stats[p.DocumentID] = s
			}
			s.Results++
			if isOut(p.Flag) {
				s.Out++
				if len(s.Names) < 4 {
					s.Names = append(s.Names, t.DisplayName)
				}
			}
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"documents": stats})
}

// biomarkerInsight explains one test: what it measures, what this person's numbers mean and what to do next.
func (m *Module) biomarkerInsight(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx, owner, profileID, code := r.Context(), httpx.Owner(r), chi.URLParam(r, "profileID"), chi.URLParam(r, "code")
	repo := observations.New(m.DB)
	byCode, names, err := repo.LoadObservationRows(ctx, owner, profileID, &code)
	if err != nil {
		httpx.WriteError(w, 500, "INSIGHTS_UNAVAILABLE", "Insights could not be prepared.", nil)
		return
	}
	now := time.Now().UTC()
	sex, age := repo.ProfileDemographics(ctx, owner, profileID, now)
	known, learned, err := repo.LoadKnowledge(ctx, code)
	if err != nil || len(byCode[code]) == 0 {
		httpx.WriteJSON(w, 200, insightSet{Items: []insightItem{}, Source: "records"})
		return
	}
	t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
	if len(t.Points) == 0 {
		httpx.WriteJSON(w, 200, insightSet{Items: []insightItem{}, Source: "records"})
		return
	}
	var b strings.Builder
	if d := m.demographics(ctx, owner, profileID); d != "" {
		b.WriteString(d + "\n")
	}
	b.WriteString("Test: " + t.DisplayName + " (" + t.CanonicalUnit + ")\n")
	for _, p := range t.Points {
		line := fmt.Sprintf("- %s: %g (%s", p.ObservedAt, p.Value, strings.ReplaceAll(p.Flag, "_", " "))
		if rt := rangeText(p.Range); rt != "" {
			line += ", range " + rt
		}
		b.WriteString(line + ")\n")
	}
	last := t.Points[len(t.Points)-1]
	fb := insightSet{Items: []insightItem{}}
	switch last.Flag {
	case "above_range", "below_range":
		dir := "above"
		if last.Flag == "below_range" {
			dir = "below"
		}
		fb.Headline = fmt.Sprintf("Your %s is %s its range.", t.DisplayName, dir)
		fb.Items = append(fb.Items, insightItem{Title: "Latest result", Detail: fmt.Sprintf("%g %s on %s, range %s.", last.Value, t.CanonicalUnit, last.ObservedAt, rangeText(last.Range)), Tone: "act"})
	default:
		fb.Headline = fmt.Sprintf("Your %s is in range.", t.DisplayName)
		fb.Items = append(fb.Items, insightItem{Title: "Latest result", Detail: fmt.Sprintf("%g %s on %s.", last.Value, t.CanonicalUnit, last.ObservedAt), Tone: "good"})
	}
	if n := len(t.Points); n > 1 {
		fb.Items = append(fb.Items, insightItem{Title: "Since last time", Detail: fmt.Sprintf("%g before, %g now across %d results.", t.Points[n-2].Value, last.Value, n), Tone: "info"})
	}
	httpx.WriteJSON(w, 200, m.insight(ctx, profileID, "biomarker", b.String(), fb))
}

// visitFacts adds the medicines and open follow-ups a doctor would ask about.
func (m *Module) visitFacts(ctx context.Context, owner, profileID string) string {
	var b strings.Builder
	if meds := m.medicationFactsFor(ctx, owner, profileID); meds != "" {
		b.WriteString(meds + "\n")
	}
	rows, err := m.DB.Pool.Query(ctx, `SELECT title FROM follow_ups WHERE profile_id=$1 AND owner_id=$2 AND status='open' ORDER BY due_on NULLS LAST LIMIT 10`, profileID, owner)
	if err == nil {
		defer rows.Close()
		var open []string
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				open = append(open, t)
			}
		}
		if len(open) > 0 {
			b.WriteString("Open follow-ups: " + strings.Join(open, "; ") + "\n")
		}
	}
	return b.String()
}

func (m *Module) medicationFactsFor(ctx context.Context, owner, profileID string) string {
	l := &labs{m: m, ctx: ctx, owner: owner, profile: profileID}
	if s := l.medicationFacts(); !strings.HasPrefix(s, "No medicines") {
		return s
	}
	return ""
}

// visitFallback is the plain summary used when the model is off: counts first, then the outside-range tests.
func visitFallback(ls []labLine) insightSet {
	out := labFallback(ls)
	n := 0
	for _, l := range ls {
		if isOut(l.Flag) {
			n++
		}
	}
	switch {
	case n == 0:
		out.Headline = "All your results are within their reference ranges."
	case n == 1:
		out.Headline = "One result is outside its reference range."
	default:
		out.Headline = fmt.Sprintf("%d results are outside their reference ranges.", n)
	}
	return out
}
