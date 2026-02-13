package bot

import (
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TradingNewsBot/analysis"
	"TradingNewsBot/storage"
)

// dailyUsage tracks how many times a user has called /analyse today.
type dailyUsage struct {
	Date  string // "2006-01-02"
	Count int
}

// Bot wraps the Telegram bot API and application dependencies.
type Bot struct {
	API              *tgbotapi.BotAPI
	Store            *storage.Store
	Analyser         *analysis.Analyser
	AnalyseWhitelist map[int64]bool

	usageMu      sync.Mutex
	analyseUsage map[int64]*dailyUsage // userID -> daily usage
}

const maxAnalysePerDay = 2

// New creates a new Bot with the given token, storage, analyser, and analyse whitelist.
func New(token string, store *storage.Store, analyser *analysis.Analyser, whitelist map[int64]bool) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	log.Printf("Authorized on Telegram as @%s", api.Self.UserName)

	return &Bot{
		API:              api,
		Store:            store,
		Analyser:         analyser,
		AnalyseWhitelist: whitelist,
		analyseUsage:     make(map[int64]*dailyUsage),
	}, nil
}

// checkAnalyseLimit returns true if the user is within their daily analyse limit.
// It also increments the counter for the current call.
func (b *Bot) checkAnalyseLimit(userID int64) (allowed bool, remaining int) {
	b.usageMu.Lock()
	defer b.usageMu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")

	usage, ok := b.analyseUsage[userID]
	if !ok || usage.Date != today {
		// New day or first use — reset
		usage = &dailyUsage{Date: today, Count: 0}
		b.analyseUsage[userID] = usage
	}

	if usage.Count >= maxAnalysePerDay {
		return false, 0
	}

	usage.Count++
	return true, maxAnalysePerDay - usage.Count
}

// Start begins polling for updates and dispatching commands.
func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.API.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID

		if !update.Message.IsCommand() {
			b.sendTyping(chatID)
			b.sendText(chatID, "Invalid input. Type /help for a list of available commands and how to use them.")
			continue
		}

		b.sendTyping(chatID)

		switch update.Message.Command() {
		case "start":
			b.handleStart(update.Message)
		case "help":
			b.handleHelp(update.Message)
		case "add":
			b.handleAdd(update.Message)
		case "remove":
			b.handleRemove(update.Message)
		case "list":
			b.handleList(update.Message)
		case "news":
			b.handleNews(update.Message)
		case "reports":
			b.handleReports(update.Message)
		case "analyse":
			b.handleAnalyse(update.Message)
		default:
			b.sendText(chatID, fmt.Sprintf("Unknown command: /%s\nType /help for a list of available commands and how to use them.", update.Message.Command()))
		}
	}
}

// sendTyping sends a "typing..." indicator to the chat.
func (b *Bot) sendTyping(chatID int64) {
	action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := b.API.Request(action); err != nil {
		log.Printf("Error sending typing action to %d: %v", chatID, err)
	}
}

// sendText is a helper to send a plain-text message to a chat.
func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.API.Send(msg); err != nil {
		log.Printf("Error sending message to %d: %v", chatID, err)
	}
}

// SendHTML sends an HTML-formatted message to a chat.
func (b *Bot) SendHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	if _, err := b.API.Send(msg); err != nil {
		log.Printf("Error sending HTML message to %d: %v", chatID, err)
	}
}
