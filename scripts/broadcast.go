package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// =====================================================
// EDIT YOUR BROADCAST MESSAGE HERE
// =====================================================
const message = `Hey there!

I just deployed the bot on gcloud, it should now be up 100% of the time. Let me know if it goes down
then niaoniaoniaoniaoooooooo. But your watchlist got resetted so pls readd your entire watchlist. Im thinking
of adding a function to add by just screenshotting and sending the stocks, and using ocr to help u add.
We shall see!`

// Set to true to use HTML formatting in the message above.
// When true, you can use <b>bold</b>, <i>italic</i>, <a href="...">links</a>.
const useHTML = false

// =====================================================
// END OF MESSAGE — no need to edit below this line
// =====================================================

// UserData mirrors the storage struct to read users.json.
type UserData struct {
	ChatID  int64    `json:"chat_id"`
	Symbols []string `json:"symbols"`
}

func main() {
	// Load .env from project root
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN not set. Make sure .env is in the project root.")
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../data"
	}

	// Read users.json
	usersFile := dataDir + "/users.json"
	data, err := os.ReadFile(usersFile)
	if err != nil {
		log.Fatalf("Could not read %s: %v", usersFile, err)
	}

	var users map[string]*UserData
	if err := json.Unmarshal(data, &users); err != nil {
		log.Fatalf("Could not parse %s: %v", usersFile, err)
	}

	if len(users) == 0 {
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
	for _, u := range users {
		msg := tgbotapi.NewMessage(u.ChatID, message)
		if useHTML {
			msg.ParseMode = tgbotapi.ModeHTML
			msg.DisableWebPagePreview = true
		}

		if _, err := bot.Send(msg); err != nil {
			log.Printf("Failed to send to %d: %v", u.ChatID, err)
			failed++
		} else {
			sent++
		}

		// Small delay to respect Telegram rate limits (30 msg/sec max)
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Printf("\nBroadcast complete: %d sent, %d failed, %d total users\n", sent, failed, len(users))
}
