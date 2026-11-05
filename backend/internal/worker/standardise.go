package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/processing"
)

type stdName struct {
	Label string `json:"label"`
	Name  string `json:"name"`
	Panel string `json:"panel"`
}

// cloudAllowed is true only when the person consented to AI processing and allowed the cloud model.
func (w *DocumentWorker) cloudAllowed(ctx context.Context, profileID string) bool {
	var ok bool
	if err := w.DB.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM consents WHERE profile_id=$1 AND purpose='ai_processing' AND granted AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()))
		AND COALESCE((SELECT granted FROM agent_permissions WHERE profile_id=$1 AND permission='cloud_model'), false)`, profileID).Scan(&ok); err != nil {
		return false
	}
	return ok
}

func key(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// standardise gives labels the catalog did not recognise their standard clinical name and panel. Only the
// labels are sent to the model, never values or identity. Any failure leaves the extraction untouched.
func (w *DocumentWorker) standardise(ctx context.Context, d documentInput, res *processing.Result, catalog []processing.CatalogEntry) {
	labels, seen := []string{}, map[string]bool{}
	for _, c := range res.Candidates {
		if c.BiomarkerCode == "" && !seen[c.Name] && len(labels) < 60 {
			seen[c.Name] = true
			labels = append(labels, c.Name)
		}
	}
	if len(labels) == 0 || !w.cloudAllowed(ctx, d.ProfileID) {
		return
	}
	body, _ := json.Marshal(map[string]any{"labels": labels})
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, embedding.ServiceURL()+"/v1/labs/standardise", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("standardise labels: %v", err)
		return
	}
	defer resp.Body.Close()
	var out struct {
		Tests []stdName `json:"tests"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
		return
	}
	byLabel := map[string]stdName{}
	for _, t := range out.Tests {
		if n := strings.TrimSpace(t.Name); n != "" && len(n) <= 100 {
			byLabel[t.Label] = t
		}
	}
	known := map[string]processing.CatalogEntry{}
	for _, e := range catalog {
		known[key(e.Name)] = e
		for _, t := range e.Terms {
			known[key(t)] = e
		}
	}
	for i := range res.Candidates {
		c := &res.Candidates[i]
		t, ok := byLabel[c.Name]
		if c.BiomarkerCode != "" || !ok {
			continue
		}
		c.Panel = strings.TrimSpace(t.Panel)
		c.Name = strings.TrimSpace(t.Name)
		if e, found := known[key(c.Name)]; found {
			c.BiomarkerCode, c.Name = e.Code, e.Name
		}
	}
}
