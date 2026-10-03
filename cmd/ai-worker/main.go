package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"tablescore-api/internal/ai"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := ai.ServeWorker(ctx, os.Getenv("AI_WORKER_SOCKET"), "codex"); err != nil {
		log.Fatal(err)
	}
}
