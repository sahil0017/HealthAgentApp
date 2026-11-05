package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cortex/backend/internal/knowledge"
	"cortex/backend/internal/objects"
	"cortex/backend/internal/store"
	"cortex/backend/internal/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()
	objectStore, err := objects.Connect(ctx)
	if err != nil {
		log.Fatalf("connect object storage: %v", err)
	}
	go knowledge.NewService(db).Run(ctx)
	go runRetention(ctx, db)
	go worker.NewHistoryWorker(db).Run(ctx)
	log.Printf("CORTEX document worker started")
	worker.NewDocumentWorker(db, objectStore).Run(ctx)
}

// runRetention applies every profile's retention policy hourly.
func runRetention(ctx context.Context, db *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		var ai, dev int64
		if err := db.Pool.QueryRow(ctx, `SELECT ai_history_removed,device_readings_removed FROM cortex_apply_retention()`).Scan(&ai, &dev); err != nil {
			log.Printf("retention: %v", err)
		} else if ai+dev > 0 {
			log.Printf("retention removed %d AI answers and %d device readings", ai, dev)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
