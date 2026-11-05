package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/repo/observations"

	"github.com/go-chi/chi/v5"
)

const (
	maxChatRunes = 1000
	maxAgentLoop = 4
	disclaimer   = "This is information from your own records, not medical advice."
)

// emergency mirrors the AI service's triage list: these never go to a model.
var emergency = regexp.MustCompile(`(?i)\b(chest (pain|pressure|tightness)|crushing pain|can'?t breathe|cannot breathe|trouble breathing|difficulty breathing|shortness of breath|face (is )?droop\w*|slurred speech|stroke|heart attack|severe bleeding|coughing (up )?blood|vomiting blood|anaphyla\w*|overdos\w*|took too many|passed out|unconscious|seizure|suicid\w*)\b`)

var modes = map[string]string{
	"explain": "Explain the person's lab results in plain language.",
	"plan":    "Suggest simple, non-medical next steps such as questions to ask, tests to track or records to upload. Do not recommend treatment or medication.",
	"visit":   "Prepare the person for a doctor visit: what changed, what is outside range, and questions to ask.",
}

type chatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Message string     `json:"message"`
	Mode    string     `json:"mode"`
	History []chatTurn `json:"history"`
}

// step is one visible stage of the agent's work.
type step struct {
	Tool    string `json:"tool"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
}

// chatAction is a tap-to-act suggestion. The server only describes it; the app carries it out through the
// normal endpoints when the user taps, so nothing changes the record without that tap.
type chatAction struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Biomarker string `json:"biomarker_code,omitempty"`
	Name      string `json:"name,omitempty"`
}

type labs struct {
	m       *Module
	ctx     context.Context
	owner   string
	profile string
	repo    *observations.Repo
	trends  map[string]biomarker.Trend
	touched map[string]bool
}

func (l *labs) load() error {
	byCode, names, err := l.repo.LoadObservationRows(l.ctx, l.owner, l.profile, nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	sex, age := l.repo.ProfileDemographics(l.ctx, l.owner, l.profile, now)
	l.trends = map[string]biomarker.Trend{}
	for code, rows := range byCode {
		known, learned, err := l.repo.LoadKnowledge(l.ctx, code)
		if err != nil {
			return err
		}
		t := biomarker.BuildTrend(code, names[code], rows, false, known, learned, sex, age, now)
		if len(t.Points) > 0 {
			l.trends[code] = t
		}
	}
	return nil
}

func (l *labs) codes() []string {
	out := make([]string, 0, len(l.trends))
	for c := range l.trends {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// find resolves a biomarker by code or by name, case-insensitively.
func (l *labs) find(q string) (biomarker.Trend, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return biomarker.Trend{}, false
	}
	for _, c := range l.codes() {
		t := l.trends[c]
		if strings.ToLower(c) == q || strings.ToLower(t.DisplayName) == q {
			return t, true
		}
	}
	for _, c := range l.codes() {
		t := l.trends[c]
		if n := strings.ToLower(t.DisplayName); strings.Contains(n, q) || strings.Contains(q, n) {
			return t, true
		}
	}
	return biomarker.Trend{}, false
}

func describeRange(p biomarker.TrendPoint) string {
	if p.Range == nil || (p.Range.Low == nil && p.Range.High == nil) {
		return "no reference range recorded"
	}
	switch {
	case p.Range.Low != nil && p.Range.High != nil:
		return fmt.Sprintf("reference %v–%v %s", *p.Range.Low, *p.Range.High, p.Range.Unit)
	case p.Range.High != nil:
		return fmt.Sprintf("reference up to %v %s", *p.Range.High, p.Range.Unit)
	}
	return fmt.Sprintf("reference from %v %s", *p.Range.Low, p.Range.Unit)
}

func flagWord(f string) string {
	switch f {
	case "above_range":
		return "above range"
	case "below_range":
		return "below range"
	case "in_range":
		return "in range"
	}
	return "no flag"
}

func trendFacts(t biomarker.Trend) string {
	last := t.Points[len(t.Points)-1]
	s := fmt.Sprintf("%s: latest %v %s on %s (%s, %s).", t.DisplayName, last.Value, t.CanonicalUnit, last.ObservedAt, flagWord(last.Flag), describeRange(last))
	if len(t.Points) > 1 {
		vals := make([]string, 0, len(t.Points))
		for _, p := range t.Points[max(0, len(t.Points)-6):] {
			vals = append(vals, fmt.Sprintf("%v", p.Value))
		}
		s += fmt.Sprintf(" Last %d readings (oldest first): %s. Trend: %s.", len(vals), strings.Join(vals, ", "), strings.ReplaceAll(t.Trend.Direction, "_", " "))
	}
	return s
}

// run executes one agent tool against the user's own record and returns the text the model may use.
func (l *labs) run(name string, args map[string]any) (st step, out string) {
	switch name {
	case "list_biomarkers":
		names := []string{}
		for _, c := range l.codes() {
			names = append(names, l.trends[c].DisplayName)
		}
		return step{name, "Looked through your record", fmt.Sprintf("%d biomarkers on file", len(names))}, "Biomarkers on file: " + strings.Join(names, ", ") + "."
	case "list_flagged_labs":
		lines := []string{}
		for _, c := range l.codes() {
			t := l.trends[c]
			if f := t.Points[len(t.Points)-1].Flag; f == "above_range" || f == "below_range" {
				lines = append(lines, trendFacts(t))
				l.touched[c] = true
			}
		}
		if len(lines) == 0 {
			return step{name, "Checked which results are outside range", "None outside range"}, "No latest result is outside its reference range."
		}
		return step{name, "Checked which results are outside range", fmt.Sprintf("%d outside range", len(lines))}, strings.Join(lines, "\n")
	case "get_trend":
		q, _ := args["name"].(string)
		t, ok := l.find(q)
		if !ok {
			return step{name, "Looked up " + q, "Not in your record"}, fmt.Sprintf("There is no verified result named %q in the record.", q)
		}
		l.touched[t.BiomarkerCode] = true
		last := t.Points[len(t.Points)-1]
		return step{name, "Read your " + t.DisplayName + " history", fmt.Sprintf("%d readings, latest %v %s", len(t.Points), last.Value, t.CanonicalUnit)}, trendFacts(t)
	}
	return step{name, "Unknown tool", "skipped"}, "Unknown tool."
}

func toolSchemas() []map[string]any {
	obj := func(props map[string]any, req ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": req}
	}
	fn := func(name, desc string, params map[string]any) map[string]any {
		return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": desc, "parameters": params}}
	}
	return []map[string]any{
		fn("list_biomarkers", "List the names of the biomarkers in the person's record.", obj(map[string]any{})),
		fn("list_flagged_labs", "List the latest results that are outside their reference range, with recent readings.", obj(map[string]any{})),
		fn("get_trend", "Get the latest value, reference range and recent readings for one biomarker.", obj(map[string]any{"name": map[string]any{"type": "string", "description": "Biomarker name, e.g. HbA1c"}}, "name")),
	}
}

func systemPrompt(mode string) string {
	return "You are a careful health-record assistant. " + modes[mode] + " " +
		"Use the tools to read the person's record and use only numbers and facts the tools return. " +
		"Never diagnose, name conditions, or recommend medication, doses or treatment. " +
		"If the record does not contain the answer, say so and suggest what to upload. " +
		"Answer in under 120 words, plain language, no markdown tables."
}

var directive = regexp.MustCompile(`(?i)\b(you have|you are diagnosed|diagnos(is|es|ed|e)|you should (take|stop|start|increase|reduce)|prescri\w*|take \d+\s*(mg|mcg|g|ml)|dosage|dose of|cure|treatment plan)\b`)
var number = regexp.MustCompile(`\d+(?:\.\d+)?`)

// grounded accepts a model answer only if every number in it came from a tool result and it holds no
// diagnosis or treatment language. Anything else is replaced by the record-based answer.
func grounded(answer, facts string) bool {
	if directive.MatchString(answer) {
		return false
	}
	have := map[string]bool{}
	for _, n := range number.FindAllString(facts, -1) {
		have[n] = true
	}
	for _, n := range number.FindAllString(answer, -1) {
		if !have[n] {
			return false
		}
	}
	return true
}

type agentReply struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"tool_calls"`
	Model string `json:"model"`
}

func (m *Module) modelStep(ctx context.Context, msgs []map[string]any) (*agentReply, bool) {
	body, _ := json.Marshal(map[string]any{"messages": msgs, "tools": toolSchemas()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embedding.ServiceURL()+"/v1/agent/step", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	var out agentReply
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
		return nil, false
	}
	return &out, true
}

type copilotResult struct {
	Answer string `json:"answer"`
	Model  string `json:"model"`
	Calls  []struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	} `json:"calls"`
}

// medicationFacts lists the person's active saved prescriptions as plain text for the agent.
func (l *labs) medicationFacts() string {
	rows, err := l.m.DB.Pool.Query(l.ctx, `SELECT i.name,i.strength,i.frequency_text,i.timing_text,i.duration_text,i.slots,i.as_needed
		FROM prescription_items i JOIN prescriptions p ON p.id=i.prescription_id
		WHERE i.profile_id=$1 AND i.active AND p.owner_id=$2 ORDER BY i.name LIMIT 40`, l.profile, l.owner)
	if err != nil {
		return "No medicines are saved in the record."
	}
	defer rows.Close()
	lines := []string{}
	for rows.Next() {
		var name, strength, freq, timing, dur string
		var slots []string
		var prn bool
		if rows.Scan(&name, &strength, &freq, &timing, &dur, &slots, &prn) != nil {
			continue
		}
		s := strings.TrimSpace(name + " " + strength)
		if freq != "" {
			s += ", " + freq
		}
		if timing != "" {
			s += ", " + timing
		}
		if len(slots) > 0 {
			s += ", daily slots: " + strings.Join(slots, "/")
		}
		if prn {
			s += ", as needed"
		}
		if dur != "" {
			s += ", for " + dur
		}
		lines = append(lines, s)
	}
	if len(lines) == 0 {
		return "No medicines are saved in the record."
	}
	return "Medicines saved from prescriptions: " + strings.Join(lines, "; ") + "."
}

func (l *labs) snapshot() map[string]any {
	var flagged []string
	trends := []map[string]string{}
	names := []string{}
	for _, c := range l.codes() {
		t := l.trends[c]
		names = append(names, t.DisplayName)
		trends = append(trends, map[string]string{"code": c, "name": t.DisplayName, "out": trendFacts(t)})
		if f := t.Points[len(t.Points)-1].Flag; f == "above_range" || f == "below_range" {
			flagged = append(flagged, trendFacts(t))
		}
	}
	flaggedText := "No latest result is outside its reference range."
	if len(flagged) > 0 {
		flaggedText = strings.Join(flagged, "\n")
	}
	return map[string]any{
		"list_biomarkers":   "Biomarkers on file: " + strings.Join(names, ", ") + ".",
		"list_flagged_labs": flaggedText,
		"trends":            trends,
		"list_medications":  l.medicationFacts(),
	}
}

// copilotStep runs one agentic turn through the GitHub Copilot SDK service. The model calls record tools that
// read a snapshot sent with the request; the answer is accepted only if it passes grounded().
func (m *Module) copilotStep(l *labs, in chatRequest) (answer, model string, steps []step, ok bool) {
	hist := []map[string]string{}
	for _, h := range in.History[max(0, len(in.History)-6):] {
		if h.Role == "user" || h.Role == "assistant" {
			hist = append(hist, map[string]string{"role": h.Role, "content": truncate(h.Content, 1200)})
		}
	}
	snap := l.snapshot()
	body, _ := json.Marshal(map[string]any{"message": in.Message, "system": systemPrompt(in.Mode), "history": hist, "snapshot": snap})
	req, err := http.NewRequestWithContext(l.ctx, http.MethodPost, embedding.ServiceURL()+"/v1/agent/copilot", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out copilotResult
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
		return
	}
	var facts strings.Builder
	for _, c := range out.Calls {
		if c.Tool == "list_medications" {
			text := snap["list_medications"].(string)
			steps = append(steps, step{c.Tool, "Checked your saved medicines", truncate(text, 90)})
			facts.WriteString(text + "\n")
			continue
		}
		st, text := l.run(c.Tool, c.Args)
		steps = append(steps, st)
		facts.WriteString(text + "\n")
	}
	if out.Answer == "" || !grounded(out.Answer, facts.String()+" "+in.Message) {
		return
	}
	return out.Answer, out.Model, steps, true
}

// recordAnswer is the answer used when no model is allowed or reachable: only facts from the record.
func (l *labs) recordAnswer(message, mode string) ([]step, string) {
	steps := []step{}
	facts := []string{}
	add := func(name string, args map[string]any) {
		st, out := l.run(name, args)
		steps = append(steps, st)
		facts = append(facts, out)
	}
	matched := false
	low := strings.ToLower(message)
	for _, c := range l.codes() {
		if n := strings.ToLower(l.trends[c].DisplayName); n != "" && strings.Contains(low, n) {
			add("get_trend", map[string]any{"name": l.trends[c].DisplayName})
			matched = true
		}
	}
	if !matched {
		add("list_flagged_labs", nil)
	}
	head := "Here is what your record shows:"
	if mode == "visit" {
		head = "For your visit, here is what stands out in your record:"
	}
	return steps, head + "\n" + strings.Join(facts, "\n")
}

func (l *labs) actions(mode string) []chatAction {
	out := []chatAction{}
	for _, c := range l.codes() {
		if !l.touched[c] {
			continue
		}
		t := l.trends[c]
		out = append(out,
			chatAction{Kind: "open_trend", Label: "Open " + t.DisplayName + " chart", Biomarker: c, Name: t.DisplayName},
			chatAction{Kind: "set_goal", Label: "Set a goal for " + t.DisplayName, Biomarker: c, Name: t.DisplayName},
			chatAction{Kind: "follow_up", Label: "Add a follow-up to discuss " + t.DisplayName, Biomarker: c, Name: t.DisplayName})
		if len(out) >= 6 {
			break
		}
	}
	if mode == "visit" || len(out) > 0 {
		out = append(out, chatAction{Kind: "visit_prep", Label: "Open visit prep"})
	}
	return out
}

func (m *Module) chat(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "profileID")
	if !m.ProfileOwned(r) {
		m.notFound(w)
		return
	}
	ctx := r.Context()
	if !m.HasConsent(ctx, profileID, "ai_processing") {
		httpx.WriteError(w, 403, "CONSENT_REQUIRED", "AI processing consent is required.", nil)
		return
	}
	if !m.granted(ctx, profileID, "read_labs") {
		httpx.WriteError(w, 403, "PERMISSION_REQUIRED", "Allow the agent to read your lab results first.", nil)
		return
	}
	var in chatRequest
	if httpx.DecodeJSON(r, 32<<10, &in) != nil || strings.TrimSpace(in.Message) == "" {
		httpx.Invalid(w, "MESSAGE_REQUIRED", "A message is required.", "message")
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if utf8.RuneCountInString(in.Message) > maxChatRunes {
		httpx.Invalid(w, "MESSAGE_TOO_LONG", "Messages can be up to 1000 characters.", "message")
		return
	}
	if _, ok := modes[in.Mode]; !ok {
		in.Mode = "explain"
	}
	if emergency.MatchString(in.Message) {
		httpx.WriteJSON(w, 200, map[string]any{
			"answer": "This may be an emergency. Call your local emergency number now or go to the nearest emergency department. I can't assess symptoms.",
			"steps":  []step{}, "actions": []chatAction{{Kind: "emergency", Label: "Open emergency"}},
			"source": "safety", "mode": in.Mode, "urgent": true, "disclaimer": disclaimer,
		})
		return
	}
	l := &labs{m: m, ctx: ctx, owner: httpx.Owner(r), profile: profileID, repo: observations.New(m.DB), touched: map[string]bool{}}
	if err := l.load(); err != nil {
		httpx.WriteError(w, 500, "RECORD_UNAVAILABLE", "Your record could not be read right now.", nil)
		return
	}
	steps := []step{}
	answer, source, model := "", "records", ""
	cloud := m.granted(ctx, profileID, "cloud_model")
	if cloud {
		if a, mod, st, ok := m.copilotStep(l, in); ok {
			answer, source, model, steps = a, "copilot", mod, st
		}
	}
	if cloud && answer == "" {
		msgs := []map[string]any{{"role": "system", "content": systemPrompt(in.Mode)}}
		for _, h := range in.History[max(0, len(in.History)-6):] {
			if h.Role == "user" || h.Role == "assistant" {
				msgs = append(msgs, map[string]any{"role": h.Role, "content": truncate(h.Content, 1200)})
			}
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": in.Message})
		var facts strings.Builder
		for i := 0; i < maxAgentLoop; i++ {
			reply, ok := m.modelStep(ctx, msgs)
			if !ok {
				break
			}
			model = reply.Model
			if len(reply.ToolCalls) == 0 {
				if reply.Content != "" && grounded(reply.Content, facts.String()+" "+in.Message) {
					answer, source = reply.Content, "model"
				}
				break
			}
			calls := make([]map[string]any, 0, len(reply.ToolCalls))
			for _, c := range reply.ToolCalls {
				calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Arguments}})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": reply.Content, "tool_calls": calls})
			for _, c := range reply.ToolCalls {
				args := map[string]any{}
				_ = json.Unmarshal([]byte(c.Arguments), &args)
				st, out := l.run(c.Name, args)
				steps = append(steps, st)
				facts.WriteString(out + "\n")
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": c.ID, "content": out})
			}
		}
	}
	if answer == "" {
		var recSteps []step
		recSteps, answer = l.recordAnswer(in.Message, in.Mode)
		if source == "records" {
			steps = recSteps
		}
		model = ""
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"answer": answer, "steps": steps, "actions": l.actions(in.Mode),
		"source": source, "model": model, "mode": in.Mode, "urgent": false, "disclaimer": disclaimer,
	})
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
