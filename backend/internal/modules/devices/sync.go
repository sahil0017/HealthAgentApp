package devices

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/domain/device"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

const (
	maxBatch       = 500
	maxRejectedOut = 50
	maxClockSkew   = 5 * time.Minute
)

var earliestReading = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)

type readingInput struct {
	Metric           string   `json:"metric"`
	Value            *float64 `json:"value"`
	Unit             string   `json:"unit"`
	MeasuredAt       string   `json:"measured_at"`
	ExternalID       string   `json:"external_id"`
	UTCOffsetMinutes int      `json:"utc_offset_minutes"`
}

type syncRequest struct {
	Readings []readingInput `json:"readings"`
}

type rejection struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

type syncResult struct {
	Accepted   int         `json:"accepted"`
	Duplicates int         `json:"duplicates"`
	Rejected   int         `json:"rejected"`
	Errors     []rejection `json:"errors"`
}

type validReading struct {
	metric, unit, origUnit, key string
	value, orig                 float64
	at                          time.Time
	day                         string
}

func dedupeKey(in readingInput, value float64, at time.Time) string {
	if id := strings.TrimSpace(in.ExternalID); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%.6f|%s", in.Metric, at.UnixNano(), value, strings.TrimSpace(in.Unit))))
	return hex.EncodeToString(sum[:])
}

// validate checks one reading and converts it to the canonical unit. The returned text is safe to show.
func validate(in readingInput, now time.Time) (validReading, string) {
	if in.Value == nil {
		return validReading{}, "value is required"
	}
	if len(in.ExternalID) > 200 {
		return validReading{}, "external_id is too long"
	}
	if in.UTCOffsetMinutes < -14*60 || in.UTCOffsetMinutes > 14*60 {
		return validReading{}, "utc_offset_minutes is out of range"
	}
	at, err := time.Parse(time.RFC3339, in.MeasuredAt)
	if err != nil {
		return validReading{}, "measured_at must be an RFC 3339 timestamp"
	}
	at = at.UTC()
	if at.After(now.Add(maxClockSkew)) {
		return validReading{}, "measured_at is in the future"
	}
	if at.Before(earliestReading) {
		return validReading{}, "measured_at is too old"
	}
	value, err := device.Normalize(in.Metric, *in.Value, in.Unit)
	if err != nil {
		return validReading{}, err.Error()
	}
	return validReading{
		metric: in.Metric, unit: canonicalUnit(in.Metric), origUnit: strings.TrimSpace(in.Unit),
		value: value, orig: *in.Value, at: at,
		day: at.Add(time.Duration(in.UTCOffsetMinutes) * time.Minute).Format("2006-01-02"),
		key: dedupeKey(in, value, at),
	}, ""
}

func canonicalUnit(metric string) string {
	m, _ := device.Lookup(metric)
	return m.Unit
}

// syncReadings stores a batch idempotently: a reading seen before is counted as a duplicate, an invalid
// one is reported by index, and neither blocks the rest of the batch.
func (m *Module) syncReadings(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedDevice(w, r)
	if !ok || !m.requireIntegrationConsent(w, r) {
		return
	}
	var status string
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT status FROM devices WHERE id=$1`, id).Scan(&status); err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	if status != "connected" {
		httpx.WriteError(w, http.StatusConflict, "DEVICE_DISCONNECTED", "Reconnect this device before syncing readings.", nil)
		return
	}
	var req syncRequest
	if httpx.DecodeJSON(r, 512<<10, &req) != nil || len(req.Readings) == 0 || len(req.Readings) > maxBatch {
		httpx.Invalid(w, "INVALID_SYNC", fmt.Sprintf("Send between 1 and %d readings.", maxBatch), "readings")
		return
	}

	now := time.Now().UTC()
	res := syncResult{Errors: []rejection{}}
	valid := make([]validReading, 0, len(req.Readings))
	for i, in := range req.Readings {
		v, reason := validate(in, now)
		if reason != "" {
			res.Rejected++
			if len(res.Errors) < maxRejectedOut {
				res.Errors = append(res.Errors, rejection{Index: i, Reason: reason})
			}
			continue
		}
		valid = append(valid, v)
	}

	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var first, last *time.Time
	for _, v := range valid {
		tag, err := tx.Exec(r.Context(), `INSERT INTO device_readings(device_id,profile_id,owner_id,metric,value,unit,original_value,original_unit,measured_at,local_date,dedupe_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (device_id,dedupe_key) DO NOTHING`,
			id, profileID, owner, v.metric, v.value, v.unit, v.orig, v.origUnit, v.at, v.day, v.key)
		if err != nil {
			httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
			return
		}
		if tag.RowsAffected() == 0 {
			res.Duplicates++
			continue
		}
		res.Accepted++
		at := v.at
		if first == nil || at.Before(*first) {
			first = &at
		}
		if last == nil || at.After(*last) {
			last = &at
		}
	}
	if _, err := tx.Exec(r.Context(), `UPDATE devices SET last_synced_at=now(),updated_at=now() WHERE id=$1`, id); err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO device_syncs(device_id,profile_id,owner_id,accepted,duplicates,rejected,first_measured_at,last_measured_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, profileID, owner, res.Accepted, res.Duplicates, res.Rejected, first, last); err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'device.synced','device',$3,jsonb_build_object('accepted',$4::int,'duplicates',$5::int,'rejected',$6::int))`, owner, profileID, id, res.Accepted, res.Duplicates, res.Rejected); err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, 500, "SYNC_FAILED", "The readings could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

type syncRecord struct {
	ID              int64      `json:"id"`
	Accepted        int        `json:"accepted"`
	Duplicates      int        `json:"duplicates"`
	Rejected        int        `json:"rejected"`
	FirstMeasuredAt *time.Time `json:"first_measured_at"`
	LastMeasuredAt  *time.Time `json:"last_measured_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// syncHistory lists past syncs of a device, newest first.
func (m *Module) syncHistory(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedDevice(w, r)
	if !ok {
		return
	}
	limit, offset, err := httpx.Page(r)
	if err != nil {
		httpx.WriteError(w, 400, "INVALID_PAGINATION", "Pagination parameters are invalid.", nil)
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT id,accepted,duplicates,rejected,first_measured_at,last_measured_at,created_at FROM device_syncs WHERE device_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		httpx.WriteError(w, 500, "SYNC_HISTORY_FAILED", "Sync history could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []syncRecord{}
	for rows.Next() {
		var s syncRecord
		if err := rows.Scan(&s.ID, &s.Accepted, &s.Duplicates, &s.Rejected, &s.FirstMeasuredAt, &s.LastMeasuredAt, &s.CreatedAt); err != nil {
			httpx.WriteError(w, 500, "SYNC_HISTORY_FAILED", "Sync history could not be loaded.", nil)
			return
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
