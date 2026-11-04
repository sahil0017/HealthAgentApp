// Package medications looks up medicines (module 22): names and RxNorm identifiers from NLM RxNav, and
// US prescribing information from openFDA. It stores nothing and sends no profile data upstream, only
// the medicine name the user typed. The sources are US catalogues, not an Indian brand list, and every
// answer says so. It may import internal/platform only.
package medications

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cortex/backend/internal/platform/core"
	"cortex/backend/internal/platform/httpx"
	"cortex/backend/internal/platform/module"

	"github.com/go-chi/chi/v5"
)

const (
	rxnavURL   = "https://rxnav.nlm.nih.gov/REST"
	openFDAURL = "https://api.fda.gov/drug/label.json"
	cacheTTL   = 6 * time.Hour
	coverage   = "Names come from the US RxNorm catalogue and prescribing text from the US FDA. Indian brands and local formulations may not appear. Ask your pharmacist or doctor before changing any medicine."
)

type Module struct {
	module.Base
	*core.Core
	HTTP     *http.Client
	RxNavURL string
	OpenFDA  string
	mu       sync.Mutex
	cache    map[string]cached
}

type cached struct {
	at   time.Time
	data any
}

func New(c *core.Core) *Module {
	return &Module{Core: c, HTTP: &http.Client{Timeout: 20 * time.Second}, RxNavURL: rxnavURL, OpenFDA: openFDAURL, cache: map[string]cached{}}
}

func (m *Module) Name() string { return "medications" }

func (m *Module) Authed(r chi.Router) {
	r.Get("/medications/search", m.search)
	r.Get("/medications/label", m.label)
}

func (m *Module) get(ctx context.Context, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "SehatApp/1.0 (personal health app)")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return 404, nil
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 200, err
	}
	return 200, json.Unmarshal(b, out)
}

func (m *Module) cached(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cache[key]
	if !ok || time.Since(c.at) > cacheTTL {
		return nil, false
	}
	return c.data, true
}

func (m *Module) store(key string, v any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) > 500 {
		m.cache = map[string]cached{}
	}
	m.cache[key] = cached{at: time.Now(), data: v}
}

func queryName(w http.ResponseWriter, r *http.Request) (string, bool) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	if len([]rune(q)) < 2 || len([]rune(q)) > 80 {
		httpx.WriteError(w, 400, "INVALID_QUERY", "Enter a medicine name of 2-80 characters.", nil)
		return "", false
	}
	return q, true
}

type medicine struct {
	RxCUI string `json:"rxcui"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

func (m *Module) drugs(ctx context.Context, name string) ([]medicine, error) {
	var in struct {
		DrugGroup struct {
			ConceptGroup []struct {
				TTY               string `json:"tty"`
				ConceptProperties []struct {
					RxCUI string `json:"rxcui"`
					Name  string `json:"name"`
				} `json:"conceptProperties"`
			} `json:"conceptGroup"`
		} `json:"drugGroup"`
	}
	if _, err := m.get(ctx, m.RxNavURL+"/drugs.json?name="+url.QueryEscape(name), &in); err != nil {
		return nil, err
	}
	out := []medicine{}
	for _, g := range in.DrugGroup.ConceptGroup {
		for _, c := range g.ConceptProperties {
			if len(out) >= 25 {
				return out, nil
			}
			out = append(out, medicine{RxCUI: c.RxCUI, Name: c.Name, Type: ttyLabel(g.TTY)})
		}
	}
	return out, nil
}

func ttyLabel(t string) string {
	switch t {
	case "SBD", "BPCK":
		return "Branded product"
	case "SCD", "GPCK":
		return "Generic product"
	case "BN":
		return "Brand name"
	case "IN", "MIN", "PIN":
		return "Ingredient"
	}
	return t
}

// search matches a typed name to RxNorm medicines, trying a spelling suggestion when nothing matches.
func (m *Module) search(w http.ResponseWriter, r *http.Request) {
	q, ok := queryName(w, r)
	if !ok {
		return
	}
	key := "s:" + strings.ToLower(q)
	if v, hit := m.cached(key); hit {
		httpx.WriteJSON(w, 200, v)
		return
	}
	list, err := m.drugs(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, 502, "MEDICATIONS_UNAVAILABLE", "Medicine lookup is unavailable right now.", nil)
		return
	}
	suggestion := ""
	if len(list) == 0 {
		var sp struct {
			SuggestionGroup struct {
				SuggestionList struct {
					Suggestion []string `json:"suggestion"`
				} `json:"suggestionList"`
			} `json:"suggestionGroup"`
		}
		if _, err := m.get(r.Context(), m.RxNavURL+"/spellingsuggestions.json?name="+url.QueryEscape(q), &sp); err == nil {
			if s := sp.SuggestionGroup.SuggestionList.Suggestion; len(s) > 0 {
				suggestion = s[0]
				list, _ = m.drugs(r.Context(), suggestion)
			}
		}
	}
	resp := map[string]any{"query": q, "medicines": list, "source": "NLM RxNav (RxNorm)", "coverage": coverage}
	if suggestion != "" {
		resp["did_you_mean"] = suggestion
	}
	m.store(key, resp)
	httpx.WriteJSON(w, 200, resp)
}

const maxText = 1500

func clip(v []string) string {
	s := strings.Join(strings.Fields(strings.Join(v, " ")), " ")
	if r := []rune(s); len(r) > maxText {
		return string(r[:maxText]) + "…"
	}
	return s
}

// label returns the FDA prescribing text for a medicine, quoted and never rephrased.
func (m *Module) label(w http.ResponseWriter, r *http.Request) {
	q, ok := queryName(w, r)
	if !ok {
		return
	}
	key := "l:" + strings.ToLower(q)
	if v, hit := m.cached(key); hit {
		httpx.WriteJSON(w, 200, v)
		return
	}
	esc := strings.ReplaceAll(q, `"`, "")
	search := fmt.Sprintf(`openfda.generic_name:"%s" openfda.brand_name:"%s"`, esc, esc)
	var in struct {
		Results []struct {
			Indications  []string `json:"indications_and_usage"`
			Boxed        []string `json:"boxed_warning"`
			Warnings     []string `json:"warnings"`
			WarningsCaut []string `json:"warnings_and_cautions"`
			Adverse      []string `json:"adverse_reactions"`
			Interactions []string `json:"drug_interactions"`
			OpenFDA      struct {
				Brand   []string `json:"brand_name"`
				Generic []string `json:"generic_name"`
			} `json:"openfda"`
		} `json:"results"`
	}
	status, err := m.get(r.Context(), m.OpenFDA+"?limit=1&search="+url.QueryEscape(search), &in)
	if err != nil {
		httpx.WriteError(w, 502, "MEDICATIONS_UNAVAILABLE", "Prescribing information is unavailable right now.", nil)
		return
	}
	if status == 404 || len(in.Results) == 0 {
		httpx.WriteError(w, 404, "LABEL_NOT_FOUND", "No FDA prescribing information was found for that name.", nil)
		return
	}
	l := in.Results[0]
	warn := l.Warnings
	if len(warn) == 0 {
		warn = l.WarningsCaut
	}
	resp := map[string]any{
		"name": q, "brand_names": l.OpenFDA.Brand, "generic_names": l.OpenFDA.Generic,
		"sections": []map[string]string{
			{"title": "What it is used for", "text": clip(l.Indications)},
			{"title": "Boxed warning", "text": clip(l.Boxed)},
			{"title": "Warnings", "text": clip(warn)},
			{"title": "Interactions", "text": clip(l.Interactions)},
			{"title": "Side effects", "text": clip(l.Adverse)},
		},
		"source": "openFDA drug labels", "coverage": coverage,
	}
	m.store(key, resp)
	httpx.WriteJSON(w, 200, resp)
}
