package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"whatsapp-summarizer/src/bot"
)

func main() {
	b, err := bot.New()
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := b.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	sig := <-sigChan
	log.Printf("Received signal: %v, initiating graceful shutdown...", sig)

	// Force-exit after 35s if Stop() does not return in time.
	// Stop() itself applies a 30s internal timeout, so 35s gives it a 5s buffer.
	forceExit := time.AfterFunc(35*time.Second, func() {
		log.Println("Graceful shutdown timed out — forcing exit")
		os.Exit(1)
	})
	defer forceExit.Stop()

	if err := b.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Bot shutdown complete")
}
