package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"cortex/backend/internal/labrange"
)

const (
	learnInterval = 10 * time.Minute
	// A range is only proposed when enough different reports print it and almost all agree.
	minRangeDocuments = 3
	minRangeAgreement = 0.8
)

// LearnFromRecords looks at verified records and queues work for the research agent. It writes
// only pending proposals and queued tasks; a person still approves everything.
func (s *Service) LearnFromRecords(ctx context.Context) error {
	if err := s.queueConversions(ctx); err != nil {
		return err
	}
	return s.proposeRanges(ctx)
}

// queueConversions asks for a conversion whenever one biomarker has verified readings in more
// than one unit and no approved conversion links them yet.
func (s *Service) queueConversions(ctx context.Context) error {
	rows, err := s.DB.Pool.Query(ctx, `SELECT a.biomarker_code,a.unit,b.unit FROM
		(SELECT DISTINCT biomarker_code,unit FROM observations WHERE verification_status='VERIFIED' AND biomarker_code IS NOT NULL) a
		JOIN (SELECT DISTINCT biomarker_code,unit FROM observations WHERE verification_status='VERIFIED' AND biomarker_code IS NOT NULL) b
		  ON a.biomarker_code=b.biomarker_code AND a.unit<b.unit
		WHERE NOT EXISTS (SELECT 1 FROM unit_conversions c WHERE c.biomarker_code=a.biomarker_code AND ((c.from_unit=a.unit AND c.to_unit=b.unit) OR (c.from_unit=b.unit AND c.to_unit=a.unit)))`)
	if err != nil {
		return err
	}
	var terms []string
	for rows.Next() {
		var code, from, to string
		if err := rows.Scan(&code, &from, &to); err != nil {
			rows.Close()
			return err
		}
		terms = append(terms, code+"|"+from+"|"+to)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, term := range terms {
		if err := s.Enqueue(ctx, "conversion", term); err != nil {
			return err
		}
	}
	return nil
}

type rangeKey struct{ Code, Unit string }

type rangeVote struct {
	Low, High *float64
	Docs      map[string]bool
}

func rangeText(low, high *float64) string {
	f := func(v *float64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(*v, 'f', -1, 64)
	}
	return f(low) + "-" + f(high)
}

// chooseRange returns the range printed by at least minRangeDocuments different reports when at
// least minRangeAgreement of the parseable reports agree on it.
func chooseRange(votes map[string]*rangeVote) (best *rangeVote, agree, total int, ok bool) {
	keys := make([]string, 0, len(votes))
	for k := range votes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := votes[k]
		total += len(v.Docs)
		if best == nil || len(v.Docs) > len(best.Docs) {
			best = v
		}
	}
	if best == nil || len(best.Docs) < minRangeDocuments || float64(len(best.Docs)) < minRangeAgreement*float64(total) {
		return nil, 0, total, false
	}
	return best, len(best.Docs), total, true
}

// proposeRanges turns the ranges printed on verified lab reports into pending general
// reference ranges. Only the range text and counts are used; no report or person is identified.
func (s *Service) proposeRanges(ctx context.Context) error {
	rows, err := s.DB.Pool.Query(ctx, `SELECT biomarker_code,unit,reference_range,document_id::text FROM observations
		WHERE verification_status='VERIFIED' AND source_type='LAB_DOCUMENT' AND biomarker_code IS NOT NULL AND document_id IS NOT NULL AND COALESCE(reference_range,'')<>''`)
	if err != nil {
		return err
	}
	groups := map[rangeKey]map[string]*rangeVote{}
	for rows.Next() {
		var code, unit, text, doc string
		if err := rows.Scan(&code, &unit, &text, &doc); err != nil {
			rows.Close()
			return err
		}
		low, high, ok := labrange.Parse(text)
		if !ok {
			continue
		}
		k := rangeKey{code, unit}
		if groups[k] == nil {
			groups[k] = map[string]*rangeVote{}
		}
		key := rangeText(low, high)
		if groups[k][key] == nil {
			groups[k][key] = &rangeVote{Low: low, High: high, Docs: map[string]bool{}}
		}
		groups[k][key].Docs[doc] = true
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for k, votes := range groups {
		best, agree, total, ok := chooseRange(votes)
		if !ok {
			continue
		}
		var approved bool
		if err := s.DB.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reference_ranges WHERE biomarker_code=$1::text AND unit=$2::text AND sex IS NULL AND age_min IS NULL AND age_max IS NULL)`, k.Code, k.Unit).Scan(&approved); err != nil {
			return err
		}
		if approved {
			continue
		}
		source := fmt.Sprintf("Printed on %d of %d reviewed lab reports", agree, total)
		payload, _ := json.Marshal(map[string]any{"code": k.Code, "unit": k.Unit, "low": best.Low, "high": best.High, "source": source})
		ev, _ := json.Marshal([]map[string]string{{"source": "Reviewed lab reports", "detail": source}})
		if err := s.Upsert(ctx, "reference_range", Proposal{Key: k.Code + "|" + k.Unit + "|" + rangeText(best.Low, best.High), Payload: payload, Evidence: ev, Agreement: agree}); err != nil {
			return err
		}
	}
	return nil
}
