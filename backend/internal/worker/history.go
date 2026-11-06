package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"cortex/backend/internal/store"

	"github.com/jackc/pgx/v5"
)

// HistoryWorker retires queued scans until report history can be reviewed and
// grounded in explicit document evidence instead of inferred from lab values.
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

func (w *HistoryWorker) ProcessOne(ctx context.Context) (bool, error) {
	var id string
	err := w.DB.Pool.QueryRow(ctx, `UPDATE history_scans SET status='skipped', updated_at=now()
		WHERE id=(SELECT id FROM history_scans WHERE status IN ('pending','running') AND run_after<=now()
			ORDER BY run_after LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id::text`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log.Printf("history scan %s: skipped; automatic condition inference is disabled", id)
	return true, nil
}
