// Package observations is the shared read access to observations, units and reference ranges. Several
// modules need the same trend inputs; they share this repository instead of importing each other.
package observations

import (
	"context"
	"time"

	"cortex/backend/internal/domain/biomarker"
	"cortex/backend/internal/store"
)

type Repo struct{ DB *store.Store }

func New(db *store.Store) *Repo { return &Repo{DB: db} }

func (r *Repo) LoadKnowledge(ctx context.Context, code string) (map[biomarker.ConversionKey]biomarker.Conversion, []biomarker.LearnedRange, error) {
	known := map[biomarker.ConversionKey]biomarker.Conversion{}
	rows, err := r.DB.Pool.Query(ctx, `SELECT from_unit,to_unit,factor::float8,"offset"::float8 FROM unit_conversions WHERE biomarker_code=$1`, code)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var from, to string
		var c biomarker.Conversion
		if err := rows.Scan(&from, &to, &c.Factor, &c.Offset); err != nil {
			rows.Close()
			return nil, nil, err
		}
		known[biomarker.ConversionKey{From: from, To: to}] = c
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	ranges := []biomarker.LearnedRange{}
	rr, err := r.DB.Pool.Query(ctx, `SELECT unit,sex,age_min,age_max,low::float8,high::float8,source FROM reference_ranges WHERE biomarker_code=$1 ORDER BY id`, code)
	if err != nil {
		return nil, nil, err
	}
	defer rr.Close()
	for rr.Next() {
		var l biomarker.LearnedRange
		if err := rr.Scan(&l.Unit, &l.Sex, &l.AgeMin, &l.AgeMax, &l.Low, &l.High, &l.Source); err != nil {
			return nil, nil, err
		}
		ranges = append(ranges, l)
	}
	return known, ranges, rr.Err()
}

func (r *Repo) LoadObservationRows(ctx context.Context, ownerID, profileID string, code *string) (map[string][]biomarker.ObsRow, map[string]string, error) {
	rows, err := r.DB.Pool.Query(ctx, `SELECT o.id::text,o.biomarker_code,COALESCE(c.display_name,o.name),o.value::float8,o.unit,COALESCE(o.reference_range,''),COALESCE(o.observed_at,o.created_at::date),o.source_type,o.verification_status,o.revision,COALESCE(o.document_id::text,''),o.observed_at IS NULL
		FROM observations o LEFT JOIN biomarker_catalog c ON c.code=o.biomarker_code
		WHERE o.profile_id=$1 AND o.owner_id=$2 AND o.biomarker_code IS NOT NULL AND ($3::text IS NULL OR o.biomarker_code=$3::text)`, profileID, ownerID, code)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byCode, names := map[string][]biomarker.ObsRow{}, map[string]string{}
	for rows.Next() {
		var r biomarker.ObsRow
		var c, name string
		if err := rows.Scan(&r.ID, &c, &name, &r.Value, &r.Unit, &r.LabRange, &r.On, &r.SourceType, &r.Status, &r.Revision, &r.DocumentID, &r.DateInferred); err != nil {
			return nil, nil, err
		}
		byCode[c] = append(byCode[c], r)
		names[c] = name
	}
	return byCode, names, rows.Err()
}

func (r *Repo) ProfileDemographics(ctx context.Context, ownerID, profileID string, now time.Time) (*string, *int) {
	var sex *string
	var birth *time.Time
	if err := r.DB.Pool.QueryRow(ctx, `SELECT sex,date_of_birth FROM profiles WHERE id=$1 AND owner_id=$2`, profileID, ownerID).Scan(&sex, &birth); err != nil {
		return nil, nil
	}
	if sex != nil && *sex != "female" && *sex != "male" {
		sex = nil
	}
	if birth == nil {
		return sex, nil
	}
	age := biomarker.AgeYears(*birth, now)
	return sex, &age
}

// Categories maps biomarker codes to their panel category; codes without one are omitted.
func (r *Repo) Categories(ctx context.Context) (map[string]string, error) {
	rows, err := r.DB.Pool.Query(ctx, `SELECT code,category FROM biomarker_catalog WHERE category IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code, category string
		if err := rows.Scan(&code, &category); err != nil {
			return nil, err
		}
		out[code] = category
	}
	return out, rows.Err()
}
