package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// =====================================================
// EDIT YOUR BROADCAST MESSAGE HERE
// =====================================================
const message = `Quick update:

You'll now see new <b>/alerts</b> and <b>/configure</b> buttons in the bot.

They do different things:

1. <b>/alerts</b> is where you add and manage price alerts for specific stocks.
2. <b>/configure</b> is where you control how often messages come through:
   - how often news digests are sent
   - how often price alerts are checked
   - your Do Not Disturb window

Important: <b>quiet hours are currently in UTC</b>, not SGT yet, so please convert the time if you're in Singapore.

Use <b>/configure</b> any time to adjust it.`

// Set to true to use HTML formatting in the message above.
// When true, you can use <b>bold</b>, <i>italic</i>, <a href="...">links</a>.
const useHTML = true

// =====================================================
// END OF MESSAGE — no need to edit below this line
// =====================================================

func main() {
	// Load .env from project root
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	env := os.Getenv("APP_ENV")
	var token string
	if env == "prod" || env == "production" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN_PROD")
	} else {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN or TELEGRAM_BOT_TOKEN_PROD not set. Set APP_ENV=prod for production.")
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		log.Fatal("SUPABASE_URL not set. Make sure .env is in the project root.")
	}

	// Connect to Supabase Postgres
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, supabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Fetch all user chat IDs
	rows, err := pool.Query(context.Background(), `SELECT chat_id FROM users`)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			log.Fatalf("Failed to scan chat_id: %v", err)
		}
		chatIDs = append(chatIDs, id)
	}

	if len(chatIDs) == 0 {
		log.Println("No users found. Nothing to send.")
		return
	}

	// Connect to Telegram
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("Failed to connect to Telegram: %v", err)
	}
	log.Printf("Connected as @%s", bot.Self.UserName)

	// Send to all users
	sent, failed := 0, 0
	for _, chatID := range chatIDs {
		msg := tgbotapi.NewMessage(chatID, message)
		if useHTML {
			msg.ParseMode = tgbotapi.ModeHTML
			msg.DisableWebPagePreview = true
		}

		if _, err := bot.Send(msg); err != nil {
			log.Printf("Failed to send to %d: %v", chatID, err)
			failed++
		} else {
			sent++
		}

		// Small delay to respect Telegram rate limits (30 msg/sec max)
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Printf("\nBroadcast complete: %d sent, %d failed, %d total users\n", sent, failed, len(chatIDs))
}
