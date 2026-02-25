package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"whatsapp-summarizer/src/bot"
)

func main() {
	b, err := bot.New()
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := b.Start(ctx); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	sig := <-sigChan
	log.Printf("Received signal: %v, shutting down...", sig)

	if err := b.Stop(); err != nil {
		log.Printf("Error stopping bot: %v", err)
	}

	log.Println("Bot shutdown complete")
}
