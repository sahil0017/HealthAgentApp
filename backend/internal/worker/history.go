package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/repo/observations"
	"cortex/backend/internal/store"
)

const maxScanAttempts = 3

// HistoryWorker reads a person's results after each newly approved report and saves what they say about
// medical history, so the History screen is already up to date when it is opened.
type HistoryWorker struct {
	DB       *store.Store
	Interval time.Duration
}

func NewHistoryWorker(db *store.Store) *HistoryWorker {
	return &HistoryWorker{DB: db, Interval: 5 * time.Second}
}

func (w *HistoryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		did, err := w.ProcessOne(ctx)
		if err != nil {
			log.Printf("history worker: %v", err)
		}
		if did {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ProcessOne claims one due scan and runs it. It reports whether it found one.
func (w *HistoryWorker) ProcessOne(ctx context.Context) (bool, error) {
	var id, profileID, owner string
	var attempts int
	err := w.DB.Pool.QueryRow(ctx, `UPDATE history_scans SET status='running', attempts=attempts+1, run_after=now()+interval '5 minutes', updated_at=now()
		WHERE id=(SELECT id FROM history_scans WHERE status IN ('pending','running') AND run_after<=now() ORDER BY run_after LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id::text, profile_id::text, owner_id, attempts`).Scan(&id, &profileID, &owner, &attempts)
	if err != nil {
		w.catchUp(ctx)
		return false, nil
	}
	status := "done"
	switch n, err := w.scan(ctx, profileID, owner); {
	case err == errNoAI:
		log.Printf("history scan %s: skipped, AI answers are not enabled for this profile", id)
		status = "skipped"
	case err != nil:
		log.Printf("history scan %s: %v", id, err)
		if attempts >= maxScanAttempts {
			status = "failed"
		} else {
			_, _ = w.DB.Pool.Exec(ctx, `UPDATE history_scans SET status='pending', run_after=now()+interval '1 minute', updated_at=now() WHERE id=$1`, id)
			return true, nil
		}
	default:
		log.Printf("history scan %s: %d findings saved", id, n)
	}
	_, err = w.DB.Pool.Exec(ctx, `UPDATE history_scans SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	return true, err
}

type scanError string

func (e scanError) Error() string { return string(e) }

const errNoAI = scanError("ai not allowed")

func (w *HistoryWorker) allowed(ctx context.Context, profileID string) bool {
	var ok bool
	if err := w.DB.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM consents WHERE profile_id=$1 AND purpose='ai_processing' AND granted AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()))
		AND COALESCE((SELECT granted FROM agent_permissions WHERE profile_id=$1 AND permission='read_labs'), false)
		AND COALESCE((SELECT granted FROM agent_permissions WHERE profile_id=$1 AND permission='cloud_model'), false)`, profileID).Scan(&ok); err != nil {
		return false
	}
	return ok
}

func (w *HistoryWorker) facts(ctx context.Context, owner, profileID string) (string, error) {
	repo := observations.New(w.DB)
	byCode, names, err := repo.LoadObservationRows(ctx, owner, profileID, nil)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	sex, age := repo.ProfileDemographics(ctx, owner, profileID, now)
	var b strings.Builder
	parts := []string{}
	if age != nil {
		parts = append(parts, fmt.Sprintf("age %d", *age))
	}
	if sex != nil && *sex != "" {
		parts = append(parts, "sex "+*sex)
	}
	if len(parts) > 0 {
		b.WriteString("Person: " + strings.Join(parts, ", ") + "\n")
	}
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	lines := 0
	for _, code := range codes {
		known, learned, err := repo.LoadKnowledge(ctx, code)
		if err != nil {
			return "", err
		}
		t := biomarker.BuildTrend(code, names[code], byCode[code], false, known, learned, sex, age, now)
		if len(t.Points) == 0 {
			continue
		}
		p := t.Points[len(t.Points)-1]
		fmt.Fprintf(&b, "- %s %g %s (%s)\n", t.DisplayName, p.Value, t.CanonicalUnit, strings.ReplaceAll(p.Flag, "_", " "))
		lines++
	}
	if lines == 0 {
		return "", nil
	}
	rows, err := w.DB.Pool.Query(ctx, `SELECT kind||': '||title FROM history_entries WHERE profile_id=$1 AND owner_id=$2 ORDER BY created_at DESC LIMIT 30`, profileID, owner)
	if err == nil {
		defer rows.Close()
		known := []string{}
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				known = append(known, s)
			}
		}
		if len(known) > 0 {
			b.WriteString("Known history: " + strings.Join(known, "; ") + "\n")
		}
	}
	return b.String(), nil
}

type historyFinding struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Tone   string `json:"tone"`
}

// scan saves new findings and returns how many it added.
func (w *HistoryWorker) scan(ctx context.Context, profileID, owner string) (int, error) {
	if !w.allowed(ctx, profileID) {
		return 0, errNoAI
	}
	facts, err := w.facts(ctx, owner, profileID)
	if err != nil || facts == "" {
		return 0, err
	}
	body, _ := json.Marshal(map[string]string{"kind": "history", "facts": facts})
	cctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, embedding.ServiceURL()+"/v1/insights", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, io.ErrUnexpectedEOF
	}
	var out struct {
		Items []historyFinding `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return 0, err
	}
	added := 0
	for _, f := range out.Items {
		title := strings.TrimSpace(f.Title)
		if f.Tone == "good" || title == "" || len(title) > 200 || added >= 8 {
			continue
		}
		detail := strings.TrimSpace(f.Detail)
		if len(detail) > 2000 {
			detail = detail[:2000]
		}
		tag, err := w.DB.Pool.Exec(ctx, `INSERT INTO history_entries(owner_id,profile_id,kind,title,notes,source) VALUES($1,$2,'condition',$3,$4,'report') ON CONFLICT DO NOTHING`, owner, profileID, title, detail)
		if err == nil {
			added += int(tag.RowsAffected())
		}
	}
	return added, nil
}

// catchUp queues the newest verified report of any profile that has never been scanned (reports approved before
// scanning existed), and queues skipped scans again once the person has turned AI on. Both are cheap no-ops when
// there is nothing to do.
func (w *HistoryWorker) catchUp(ctx context.Context) {
	if _, err := w.DB.Pool.Exec(ctx, `INSERT INTO history_scans(document_id,profile_id,owner_id)
		SELECT DISTINCT ON (o.profile_id) o.document_id, o.profile_id, d.owner_id
		FROM observations o JOIN medical_documents d ON d.id=o.document_id
		WHERE o.verification_status='VERIFIED' AND o.document_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM history_scans h WHERE h.profile_id=o.profile_id)
		ORDER BY o.profile_id, d.created_at DESC
		ON CONFLICT (document_id) DO NOTHING`); err != nil {
		log.Printf("history catch-up: %v", err)
	}
	if _, err := w.DB.Pool.Exec(ctx, `UPDATE history_scans h SET status='pending', attempts=0, run_after=now(), updated_at=now()
		WHERE h.status='skipped'
		AND EXISTS(SELECT 1 FROM consents c WHERE c.profile_id=h.profile_id AND c.purpose='ai_processing' AND c.granted AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>now()))
		AND COALESCE((SELECT granted FROM agent_permissions p WHERE p.profile_id=h.profile_id AND p.permission='cloud_model'), false)
		AND COALESCE((SELECT granted FROM agent_permissions p WHERE p.profile_id=h.profile_id AND p.permission='read_labs'), false)`); err != nil {
		log.Printf("history re-queue: %v", err)
	}
}
