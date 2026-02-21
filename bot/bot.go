package bot

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TradingNewsBot/analysis"
	"TradingNewsBot/storage"
)

// pendingAction values: we're waiting for the user to reply with a symbol or "all".
const (
	pendingAdd     = "add"
	pendingRemove  = "remove"
	pendingNews    = "news"
	pendingAnalyse = "analyse"
	pendingPrice   = "price"
	pendingAlerts  = "alerts"
	pendingDND     = "dnd"
)

// Bot wraps the Telegram bot API and application dependencies.
type Bot struct {
	API              *tgbotapi.BotAPI
	Store            *storage.Store
	Analyser            *analysis.Analyser
	AnalyseWhitelist    map[int64]bool
	WatchlistWhitelist  map[int64]bool // user IDs that get 20 watchlist slots instead of 10
	OpenAIAPIKey        string

	pendingMu       sync.Mutex
	pendingAction   map[int64]string // chatID -> "add" | "remove" | "news" | "analyse"
	pendingUserID   map[int64]int64  // chatID -> userID (for rate limit when handling pending)
}

const maxAnalysePerDay = 5

// getAnalyseLimitStatus returns whether the user can analyse and how many remain. Does NOT increment.
// Whitelisted users (ANALYSE_WHITELIST) bypass the limit entirely.
func (b *Bot) getAnalyseLimitStatus(userID int64) (allowed bool, remaining int) {
	if b.AnalyseWhitelist[userID] {
		return true, maxAnalysePerDay
	}
	_, remaining, err := b.Store.GetAnalyseUsage(userID, maxAnalysePerDay)
	if err != nil {
		log.Printf("GetAnalyseUsage error: %v", err)
		return true, maxAnalysePerDay // Proceed on error to avoid blocking
	}
	return remaining > 0, remaining
}

// getSlotLimit returns max watchlist slots for the user (20 if whitelisted or paid for expansion, 10 otherwise).
func (b *Bot) getSlotLimit(chatID int64) int {
	if b.WatchlistWhitelist != nil && b.WatchlistWhitelist[chatID] {
		return 20
	}
	expanded, err := b.Store.HasSlotsExpanded(chatID)
	if err != nil {
		log.Printf("HasSlotsExpanded error: %v", err)
		return 10
	}
	if expanded {
		return 20
	}
	return 10
}

// consumeAnalyseSlot increments the daily count in DB. Call only once per actual analysis (in doAnalyse).
// Whitelisted users bypass the limit and do not consume a slot.
func (b *Bot) consumeAnalyseSlot(userID int64) (allowed bool, remaining int) {
	if b.AnalyseWhitelist[userID] {
		return true, maxAnalysePerDay
	}
	allowed, remaining, err := b.Store.ConsumeAnalyseSlot(userID, maxAnalysePerDay)
	if err != nil {
		log.Printf("ConsumeAnalyseSlot error: %v", err)
		return false, 0
	}
	return allowed, remaining
}

// New creates a new Bot with the given token, storage, analyser, whitelists, and OpenAI API key.
func New(token string, store *storage.Store, analyser *analysis.Analyser, analyseWhitelist, watchlistWhitelist map[int64]bool, openaiKey string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	log.Printf("Authorized on Telegram as @%s", api.Self.UserName)

	return &Bot{
		API:                 api,
		Store:               store,
		Analyser:            analyser,
		AnalyseWhitelist:    analyseWhitelist,
		WatchlistWhitelist:  watchlistWhitelist,
		OpenAIAPIKey:        openaiKey,
		pendingAction:       make(map[int64]string),
		pendingUserID:       make(map[int64]int64),
	}, nil
}

// downloadFile fetches a file from Telegram by file ID and returns its bytes.
func (b *Bot) downloadFile(fileID string) ([]byte, error) {
	file, err := b.API.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}
	url := file.Link(b.API.Token)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// requireEligible checks subscription status. If not eligible, sends paywall and invoice, returns true (blocked).
// Caller should return when true. Returns false when eligible (proceed with command).
func (b *Bot) requireEligible(chatID int64) bool {
	if err := b.Store.RecordFirstUse(chatID); err != nil {
		log.Printf("RecordFirstUse error: %v", err)
	}
	ok, err := b.Store.IsEligible(chatID)
	if err != nil {
		log.Printf("IsEligible error: %v", err)
		return false // Proceed on error to avoid blocking users
	}
	if !ok {
		b.sendText(chatID, "Subscribe for 100 Stars/month to use TradingNewsBot — watchlist, news, earnings, AI analysis.\n\nTap the button below to pay with Telegram Stars.")
		b.sendSubscribeInvoice(chatID)
		return true
	}
	return false
}

// Start begins polling for updates and dispatching commands.
func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.API.GetUpdatesChan(u)

	for update := range updates {
		// Handle PreCheckoutQuery (payment flow)
		if update.PreCheckoutQuery != nil {
			b.handlePreCheckout(update.PreCheckoutQuery)
			continue
		}

		// Handle CallbackQuery (inline keyboard button presses)
		if update.CallbackQuery != nil {
			b.handleCallbackQuery(update.CallbackQuery)
			continue
		}

		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID

		// Handle successful payment
		if update.Message.SuccessfulPayment != nil {
			b.handleSuccessfulPayment(chatID, update.Message.SuccessfulPayment)
			continue
		}

		text := strings.TrimSpace(update.Message.Text)

		// Handle photo (watchlist image when in /add flow)
		if len(update.Message.Photo) > 0 {
			if b.requireEligible(chatID) {
				continue
			}
			b.pendingMu.Lock()
			action := b.pendingAction[chatID]
			if action == pendingAdd {
				fileID := update.Message.Photo[len(update.Message.Photo)-1].FileID
				delete(b.pendingAction, chatID)
				delete(b.pendingUserID, chatID)
				b.pendingMu.Unlock()
				b.sendTyping(chatID)
				b.handleAddFromPhoto(chatID, fileID)
				continue
			}
			b.pendingMu.Unlock()
			b.sendTyping(chatID)
			b.sendText(chatID, "Send /add first, then send a photo of your watchlist to add multiple stocks at once.")
			continue
		}

		if !update.Message.IsCommand() {
			if b.requireEligible(chatID) {
				continue
			}
			// Check if we're waiting for a symbol (add/remove)
			b.pendingMu.Lock()
			action := b.pendingAction[chatID]
			if action != "" {
				userID := b.pendingUserID[chatID]
				delete(b.pendingAction, chatID)
				delete(b.pendingUserID, chatID)
				b.pendingMu.Unlock()
				b.sendTyping(chatID)
				b.handlePendingSymbol(chatID, userID, action, text)
				continue
			}
			b.pendingMu.Unlock()

			b.sendTyping(chatID)
			b.sendText(chatID, "Invalid input. Type /help for a list of available commands and how to use them.")
			continue
		}

		b.sendTyping(chatID)

		switch update.Message.Command() {
		case "start":
			b.handleStart(update.Message)
		case "help":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleHelp(update.Message)
		case "add":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleAdd(update.Message)
		case "remove":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleRemove(update.Message)
		case "list":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleList(update.Message)
		case "news":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleNews(update.Message)
		case "analyse":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleAnalyse(update.Message)
		case "reports":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleReports(update.Message)
		case "price":
			if b.requireEligible(chatID) {
				continue
			}
			b.handlePrice(update.Message)
		case "alerts":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleAlerts(update.Message)
		case "settings":
			if b.requireEligible(chatID) {
				continue
			}
			b.handleSettings(update.Message)
		case "support":
			b.handleSupport(update.Message)
		case "terms":
			b.handleTerms(update.Message)
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
