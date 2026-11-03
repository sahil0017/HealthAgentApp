// Package knowledge stores what the research agent learns. Everything it proposes is pending until a human approves it.
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cortex/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

const discoverBatch = 40

type Proposal struct {
	Key       string          `json:"key"`
	Payload   json.RawMessage `json:"payload"`
	Evidence  json.RawMessage `json:"evidence"`
	Agreement int             `json:"agreement"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient() *Client {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("AI_SERVICE_URL")), "/")
	if base == "" {
		base = "http://localhost:8000"
	}
	return &Client{BaseURL: base, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("research service returned %d for %s", resp.StatusCode, path)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

func (c *Client) Biomarker(ctx context.Context, term, medlineURL string) (*Proposal, error) {
	var out struct {
		Proposal *Proposal `json:"proposal"`
	}
	var link any
	if medlineURL != "" {
		link = medlineURL
	}
	err := c.call(ctx, http.MethodPost, "/v1/research/biomarker", map[string]any{"term": term, "medlineplus_url": link}, &out)
	return out.Proposal, err
}

func (c *Client) Literature(ctx context.Context, term string) ([]Proposal, error) {
	var out struct {
		Proposals []Proposal `json:"proposals"`
	}
	err := c.call(ctx, http.MethodPost, "/v1/research/literature", map[string]any{"term": term}, &out)
	return out.Proposals, err
}

func (c *Client) Discover(ctx context.Context, limit, offset int) ([]Proposal, int, error) {
	var out struct {
		Proposals []Proposal `json:"proposals"`
		Total     int        `json:"total"`
	}
	err := c.call(ctx, http.MethodPost, "/v1/research/discover", map[string]any{"limit": limit, "offset": offset}, &out)
	return out.Proposals, out.Total, err
}

func (c *Client) UnitValid(ctx context.Context, unit string) (bool, error) {
	var out struct {
		Valid bool `json:"valid_ucum"`
	}
	err := c.call(ctx, http.MethodGet, "/v1/research/unit?unit="+url.QueryEscape(unit), nil, &out)
	return out.Valid, err
}

// Conversion is a unit relation derived by the research service: to = from*Factor + Offset.
type Conversion struct {
	Factor   float64           `json:"factor"`
	Offset   float64           `json:"offset"`
	Evidence []json.RawMessage `json:"evidence"`
}

func (c *Client) Convert(ctx context.Context, from, to string) (*Conversion, error) {
	var out struct {
		Conversion *Conversion `json:"conversion"`
	}
	err := c.call(ctx, http.MethodGet, "/v1/research/convert?from_unit="+url.QueryEscape(from)+"&to_unit="+url.QueryEscape(to), nil, &out)
	return out.Conversion, err
}

type Service struct {
	DB     *store.Store
	Client *Client
}

func NewService(db *store.Store) *Service { return &Service{DB: db, Client: NewClient()} }

// Enqueue is idempotent: a (kind, term) pair is researched once unless it previously failed.
func (s *Service) Enqueue(ctx context.Context, kind, term string) error {
	term = strings.TrimSpace(term)
	if term == "" || len(term) > 200 {
		return fmt.Errorf("invalid research term")
	}
	_, err := s.DB.Pool.Exec(ctx, `INSERT INTO research_tasks(kind,term) VALUES($1,$2)
		ON CONFLICT(kind,term) DO UPDATE SET status='queued', attempts=0, available_at=now(), updated_at=now()
		WHERE research_tasks.status='failed'`, kind, term)
	return err
}

// Upsert stores a proposal as pending. Decisions already made by a reviewer are never overwritten.
func (s *Service) Upsert(ctx context.Context, kind string, p Proposal) error {
	if p.Key == "" || len(p.Payload) == 0 {
		return fmt.Errorf("empty proposal")
	}
	ev := p.Evidence
	if len(ev) == 0 {
		ev = json.RawMessage(`[]`)
	}
	_, err := s.DB.Pool.Exec(ctx, `INSERT INTO knowledge_entries(kind,entry_key,payload,evidence,agreement) VALUES($1::text,$2::text,$3::text::jsonb,$4::text::jsonb,$5::int)
		ON CONFLICT(kind,entry_key) DO UPDATE SET payload=EXCLUDED.payload, evidence=EXCLUDED.evidence, agreement=EXCLUDED.agreement
		WHERE knowledge_entries.status='pending'`, kind, p.Key, string(p.Payload), string(ev), p.Agreement)
	return err
}

// Bootstrap starts discovery on a fresh install. It does nothing once biomarkers exist or discovery was already queued.
func (s *Service) Bootstrap(ctx context.Context) error {
	var known int
	if err := s.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM biomarker_catalog WHERE status='approved')+(SELECT count(*) FROM knowledge_entries WHERE kind='biomarker')+(SELECT count(*) FROM research_tasks WHERE kind='discover')`).Scan(&known); err != nil {
		return err
	}
	if known > 0 {
		return nil
	}
	return s.Enqueue(ctx, "discover", "0")
}

// ProcessOne claims and runs one research task. It reports whether a task was found.
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	var id, kind, term string
	var attempts int
	err := s.DB.Pool.QueryRow(ctx, `UPDATE research_tasks SET status='running', attempts=attempts+1, updated_at=now()
		WHERE id=(SELECT id FROM research_tasks WHERE (status='queued' AND available_at<=now()) OR (status='running' AND updated_at<now()-interval '10 minutes')
		          ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id::text, kind, term, attempts`).Scan(&id, &kind, &term, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if runErr := s.run(ctx, kind, term); runErr != nil {
		status := "queued"
		if attempts >= 4 {
			status = "failed"
		}
		delay := attempts * attempts * 30
		_, _ = s.DB.Pool.Exec(ctx, `UPDATE research_tasks SET status=$2::text, last_error=$3::text, available_at=now()+make_interval(secs => $4::int), updated_at=now() WHERE id=$1::uuid`, id, status, truncate(runErr.Error(), 300), delay)
		return true, nil
	}
	_, err = s.DB.Pool.Exec(ctx, `UPDATE research_tasks SET status='done', last_error=NULL, updated_at=now() WHERE id=$1::uuid`, id)
	return true, err
}

func (s *Service) run(ctx context.Context, kind, term string) error {
	switch kind {
	case "discover":
		offset, err := strconv.Atoi(term)
		if err != nil {
			return err
		}
		proposals, total, err := s.Client.Discover(ctx, discoverBatch, offset)
		if err != nil {
			return err
		}
		for _, p := range proposals {
			if err := s.Upsert(ctx, "biomarker", p); err != nil {
				return err
			}
		}
		if next := offset + discoverBatch; next < total {
			return s.Enqueue(ctx, "discover", strconv.Itoa(next))
		}
		return nil
	case "biomarker":
		p, err := s.Client.Biomarker(ctx, term, "")
		if err != nil || p == nil {
			return err
		}
		return s.Upsert(ctx, "biomarker", *p)
	case "literature":
		proposals, err := s.Client.Literature(ctx, term)
		if err != nil {
			return err
		}
		for _, p := range proposals {
			if err := s.Upsert(ctx, "research_source", p); err != nil {
				return err
			}
		}
		return nil
	case "conversion":
		parts := strings.Split(term, "|")
		if len(parts) != 3 {
			return fmt.Errorf("invalid conversion task")
		}
		conv, err := s.Client.Convert(ctx, parts[1], parts[2])
		if err != nil || conv == nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"code": parts[0], "from_unit": parts[1], "to_unit": parts[2], "factor": conv.Factor, "offset": conv.Offset, "source": "UCUM (NLM)"})
		ev, _ := json.Marshal(conv.Evidence)
		return s.Upsert(ctx, "unit_conversion", Proposal{Key: term, Payload: payload, Evidence: ev, Agreement: 1})
	case "unit":
		code, unit, ok := strings.Cut(term, "|")
		if !ok {
			return fmt.Errorf("invalid unit task")
		}
		valid, err := s.Client.UnitValid(ctx, unit)
		if err != nil || !valid {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"code": code, "unit": unit})
		ev, _ := json.Marshal([]map[string]string{{"source": "UCUM", "url": "https://ucum.nlm.nih.gov/", "detail": "Valid UCUM unit seen on a reviewed lab observation"}})
		return s.Upsert(ctx, "unit", Proposal{Key: code + "|" + unit, Payload: payload, Evidence: ev, Agreement: 1})
	}
	return fmt.Errorf("unknown research task %q", kind)
}

// Run drives the queue until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if err := s.Bootstrap(ctx); err != nil {
		log.Printf("research bootstrap: %v", err)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastLearn time.Time
	for {
		if time.Since(lastLearn) >= learnInterval {
			lastLearn = time.Now()
			if err := s.LearnFromRecords(ctx); err != nil {
				log.Printf("research learning: %v", err)
			}
		}
		for {
			worked, err := s.ProcessOne(ctx)
			if err != nil {
				log.Printf("research worker: %v", err)
			}
			if !worked || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
