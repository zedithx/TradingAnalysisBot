package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"TradingNewsBot/analysis"
	"TradingNewsBot/bot"
	"TradingNewsBot/config"
	"TradingNewsBot/scheduler"
	"TradingNewsBot/storage"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize storage
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	log.Printf("Storage loaded from %s", cfg.DataDir)

	// Initialize AI analyser
	analyser := analysis.New(cfg.OpenAIAPIKey)

	// Initialize Telegram bot
	b, err := bot.New(cfg.TelegramBotToken, store, analyser, cfg.AnalyseWhitelist)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	// Initialize and start the background news fetcher.
	// Pass the bot's sendHTML as the notify callback so users get digest messages.
	sched := scheduler.New(store, b.SendHTML)
	if err := sched.Start(); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}

	// Start a minimal HTTP server for Cloud Run health checks.
	// Cloud Run requires the container to listen on $PORT.
	// This has no effect when running locally without PORT set.
	go func() {
		port := os.Getenv("PORT")
		if port == "" {
			return // Not running on Cloud Run; skip HTTP server
		}
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		log.Printf("Health check server listening on :%s", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Printf("Health check server error: %v", err)
		}
	}()

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %s, shutting down...", sig)
		sched.Stop()
		os.Exit(0)
	}()

	// Start polling for Telegram updates (blocks)
	log.Println("Bot is running. Press Ctrl+C to stop.")
	b.Start()
}
