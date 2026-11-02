package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	log.Printf("CORTEX document worker started")
	worker.NewDocumentWorker(db, objectStore).Run(ctx)
}
