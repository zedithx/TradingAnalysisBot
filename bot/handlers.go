package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TradingNewsBot/vision"
	"TradingNewsBot/yahoo"
)

// sendStarsInvoice sends a Telegram Stars invoice. provider_token is omitted (required for Pay button).
func (b *Bot) sendStarsInvoice(chatID int64, title, description, payload string, prices []tgbotapi.LabeledPrice) {
	params := make(tgbotapi.Params)
	params["chat_id"] = strconv.FormatInt(chatID, 10)
	params["title"] = title
	params["description"] = description
	params["payload"] = payload
	params["currency"] = "XTR"
	// provider_token intentionally omitted — Telegram requires it absent for Stars (Pay button shows)
	if err := params.AddInterface("prices", prices); err != nil {
		log.Printf("sendStarsInvoice AddInterface error: %v", err)
		return
	}
	resp, err := b.API.MakeRequest("sendInvoice", params)
	if err != nil {
		log.Printf("sendStarsInvoice error: %v", err)
		return
	}
	if !resp.Ok {
		log.Printf("sendStarsInvoice API error: %s", resp.Description)
	}
}

// sendSubscribeInvoice sends a Telegram Stars invoice for 100 Stars/month subscription.
func (b *Bot) sendSubscribeInvoice(chatID int64) {
	b.sendStarsInvoice(chatID,
		"TradingNewsBot Premium — 1 month",
		"Unlimited access to watchlist, news, earnings reminders, and AI analysis.",
		fmt.Sprintf("sub:%d", chatID),
		[]tgbotapi.LabeledPrice{{Label: "1 month", Amount: 100}},
	)
}

// sendSlotsExpandInvoice sends a Telegram Stars invoice for 50 Stars — expand watchlist to 20 slots for 1 month.
func (b *Bot) sendSlotsExpandInvoice(chatID int64) {
	b.sendStarsInvoice(chatID,
		"Expand watchlist — 20 slots for 1 month",
		"Add up to 20 stocks instead of 10. Lasts 30 days.",
		fmt.Sprintf("slots:%d", chatID),
		[]tgbotapi.LabeledPrice{{Label: "1 month", Amount: 50}},
	)
}

// handlePreCheckout responds to the pre-checkout query. Must answer within 10 seconds or payment is cancelled.
func (b *Bot) handlePreCheckout(query *tgbotapi.PreCheckoutQuery) {
	cfg := tgbotapi.PreCheckoutConfig{
		PreCheckoutQueryID: query.ID,
		OK:                 true,
	}
	if _, err := b.API.Request(cfg); err != nil {
		log.Printf("handlePreCheckout FAILED (payment cancelled): %v", err)
		return
	}
	log.Printf("handlePreCheckout OK for query %s", query.ID)
}

// handleSuccessfulPayment processes a successful payment and extends subscription or slots expansion.
func (b *Bot) handleSuccessfulPayment(chatID int64, payment *tgbotapi.SuccessfulPayment) {
	payload := payment.InvoicePayload
	parsedChatID := chatID
	if idx := strings.Index(payload, ":"); idx >= 0 {
		if idStr := strings.TrimSpace(payload[idx+1:]); idStr != "" {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				parsedChatID = id
			}
		}
	}

	switch {
	case strings.HasPrefix(payload, "sub:"):
		if err := b.Store.ExtendSubscription(parsedChatID); err != nil {
			log.Printf("ExtendSubscription error: %v", err)
			b.sendText(parsedChatID, "Payment received but there was an error activating your subscription. Contact me on Reddit: u/Logical-Albatross873")
			return
		}
		b.sendText(parsedChatID, "Thanks! Your subscription is active for 30 days. Use /start to get going.")

	case strings.HasPrefix(payload, "slots:"):
		if err := b.Store.ExtendSlotsExpansion(parsedChatID); err != nil {
			log.Printf("ExtendSlotsExpansion error: %v", err)
			b.sendText(parsedChatID, "Payment received but there was an error. Contact me on Reddit: u/Logical-Albatross873")
			return
		}
		b.sendText(parsedChatID, "Thanks! Your watchlist is now expanded to 20 slots for 30 days. Add more stocks with /add.")

	default:
		log.Printf("Unexpected invoice payload: %s", payload)
	}
}

// handleSupport responds to /support (payment issues).
func (b *Bot) handleSupport(msg *tgbotapi.Message) {
	b.sendText(msg.Chat.ID, "For payment or subscription issues, contact me on Reddit: u/Logical-Albatross873. Telegram support cannot help with purchases made through this bot.")
}

// handleTerms responds to /terms.
func (b *Bot) handleTerms(msg *tgbotapi.Message) {
	b.sendText(msg.Chat.ID, "By subscribing, you agree that this bot is for informational purposes only and does not constitute financial advice. Subscription is non-refundable except as required by law. Contact me on Reddit (u/Logical-Albatross873) for disputes.")
}

// handleStart sends a welcome message.
func (b *Bot) handleStart(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	// Record first use and check eligibility
	_ = b.Store.RecordFirstUse(chatID)
	eligible, err := b.Store.IsEligible(chatID)
	if err != nil {
		log.Printf("IsEligible error in handleStart: %v", err)
	}
	if !eligible {
		b.sendText(chatID, "Hey! To unlock your watchlist, news, earnings reminders, and AI analysis — it’s 100 Stars/month after a 30-day free trial. Tap below to subscribe with Telegram Stars.")
		b.sendSubscribeInvoice(chatID)
		return
	}

	welcome := `Hey — glad you're here. 👋

I bring together news, earnings dates, price alerts, and AI analysis for the stocks you care about — all in one place.

You'll get news summaries every 4 hours, morning snapshots, price alerts, and a weekly recap — all sent automatically.

<b>Getting started</b>
/add — Add stocks to your watchlist (type a ticker like AAPL, or send a screenshot)
/list — View your current watchlist

<b>Stay informed</b>
/news — Latest headlines for your watchlist
/price — Live price quotes
/reports — Upcoming earnings dates
/alerts — Set price alerts (e.g. /alerts AAPL 5%)

<b>AI-powered</b>
/analyse — Deep-dive analysis on any stock

<b>Customise</b>
/configure — Digest frequency, alert frequency, DND, manage alerts

/help — Full details on every command
/support — Payment or subscription help

First 30 days are free, then 100 Stars/month. (Info only — not financial advice.)

Start by adding a few stocks — just type /add and reply with a ticker.`
	welcomeMsg := tgbotapi.NewMessage(chatID, welcome)
	welcomeMsg.ParseMode = "HTML"
	welcomeMsg.ReplyMarkup = persistentKeyboard()
	if _, err := b.API.Send(welcomeMsg); err != nil {
		log.Printf("Error sending welcome to %d: %v", chatID, err)
	}
}

// helpOverviewText is the main /help menu text.
const helpOverviewText = `Choose a topic to learn more:

First 30 days free, then 100 Stars/month.
/support for payment questions.`

// helpSections maps callback data keys to help section content.
var helpSections = map[string]string{
	"watchlist": `<b>Build Your Watchlist</b>

/add - Reply with ticker or send a photo of your watchlist
/add SYMBOL - Track a stock (e.g. /add AAPL)
/remove SYMBOL - Stop tracking a stock
/list - View your current watchlist

Use exchange suffixes for non-US stocks:
  005930.KS (Samsung, Korea)
  0700.HK (Tencent, Hong Kong)
  D05.SI (DBS, Singapore)`,

	"news": `<b>News &amp; Sentiment</b>

/news - Latest 10 headlines across your watchlist
/news SYMBOL - Latest 10 headlines for any stock

News headlines are one of the most important tools for gauging market sentiment. Negative news (lawsuits, missed guidance, regulatory action) often drives selling pressure. Positive catalysts (partnerships, product launches, analyst upgrades) can signal buying momentum. Use /news regularly to stay ahead of price-moving events.`,

	"earnings": `<b>Earnings Dates</b>

/reports - Next earnings report dates for your watchlist

Earnings reports (Q1–Q4) are when companies publish financials — revenue, net income, expenses, and guidance. Strong earnings validate growth; misses can trigger sharp sell-offs. Knowing report dates helps you prepare for volatility.`,

	"price": `<b>Live Price</b>

/price SYMBOL - Live price, % change, pre/post market, day range, volume
/price - Reply with symbol or "all" for your whole watchlist`,

	"analysis": `<b>AI Analysis</b>

/analyse SYMBOL - AI-powered analysis for a specific stock
/analyse - AI analysis for all your watchlist stocks

Consolidates recent news, live price data, and earnings info, then uses AI to provide a sentiment summary, key risk factors, and a short-term outlook.

For fresh headlines and live market updates, use /news and /price.`,

	"alerts": `<b>Price Alerts</b>

/alerts - View, add, or remove price alerts
/alerts AAPL 5% - Alert when AAPL moves ±5% from previous close
/alerts AAPL 150 - Alert when AAPL hits $150

Set per-stock thresholds to get notified when a price target or percentage move is hit. Alerts are checked during market hours — control how often they fire in /configure.`,

	"settings": `<b>Settings</b>

/configure - All-in-one settings: digest frequency, alert frequency, DND, manage alerts

Set how often news digests are sent (2h/4h/8h/24h), how often price alerts are checked (1h/2h/4h/8h), set a Do Not Disturb window, and manage your per-stock price alerts.`,
}

// helpKeyboard returns the inline keyboard for the /help overview.
func helpKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Watchlist", "help:watchlist"),
			tgbotapi.NewInlineKeyboardButtonData("News", "help:news"),
			tgbotapi.NewInlineKeyboardButtonData("Earnings", "help:earnings"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Price", "help:price"),
			tgbotapi.NewInlineKeyboardButtonData("AI Analysis", "help:analysis"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Price Alerts", "help:alerts"),
			tgbotapi.NewInlineKeyboardButtonData("Settings", "help:settings"),
		),
	)
}

// handleHelp sends the interactive help menu.
func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	kb := helpKeyboard()
	out := tgbotapi.NewMessage(msg.Chat.ID, helpOverviewText)
	out.ReplyMarkup = kb
	b.API.Send(out)
}

// handleAdd validates and adds a stock symbol to the user's watchlist.
// If no symbol is provided (e.g. user clicked /add in menu), prompts them to reply with one.
func (b *Bot) handleAdd(msg *tgbotapi.Message) {
	symbol := strings.TrimSpace(msg.CommandArguments())
	if symbol == "" {
		b.pendingMu.Lock()
		b.pendingAction[msg.Chat.ID] = pendingAdd
		b.pendingMu.Unlock()
		b.sendText(msg.Chat.ID, "Reply with a ticker (e.g. AAPL) or send a photo of your watchlist to add multiple stocks at once.")
		return
	}
	b.doAdd(msg.Chat.ID, symbol)
}

// handleAddFromPhoto extracts symbols from a watchlist image and adds them.
func (b *Bot) handleAddFromPhoto(chatID int64, fileID string) {
	b.sendText(chatID, "Scanning your watchlist...")

	imageBytes, err := b.downloadFile(fileID)
	if err != nil {
		log.Printf("Download photo error: %v", err)
		b.sendText(chatID, "Could not download the image. Please try again.")
		return
	}

	symbols, err := vision.ExtractSymbols(imageBytes, b.OpenAIAPIKey)
	if err != nil {
		log.Printf("Vision extract error: %v", err)
		b.sendText(chatID, "Could not extract symbols from the image. Try typing them one by one.")
		return
	}

	if len(symbols) == 0 {
		b.sendText(chatID, "No stock symbols found in the image. Try a clearer screenshot or type them one by one.")
		return
	}

	existing := b.Store.GetSymbols(chatID)
	slotLimit := b.getSlotLimit(chatID)
	added, skipped, invalid := []string{}, []string{}, []string{}

	for _, sym := range symbols {
		if len(existing)+len(added) >= slotLimit {
			skipped = append(skipped, sym+" (limit reached)")
			continue
		}
		// Check duplicate in existing or already-added
		dup := false
		for _, s := range existing {
			if s == sym {
				dup = true
				break
			}
		}
		for _, s := range added {
			if s == sym {
				dup = true
				break
			}
		}
		if dup {
			skipped = append(skipped, sym+" (already in watchlist)")
			continue
		}
		if err := yahoo.ValidateSymbol(sym); err != nil {
			invalid = append(invalid, sym)
			continue
		}
		if err := b.Store.AddSymbol(chatID, sym); err != nil {
			invalid = append(invalid, sym)
			continue
		}
		added = append(added, sym)
	}

	// Build result message
	var parts []string
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("Added: %s", strings.Join(added, ", ")))
	}
	if len(invalid) > 0 {
		parts = append(parts, fmt.Sprintf("Skipped (not found): %s", strings.Join(invalid, ", ")))
	}
	if len(skipped) > 0 {
		parts = append(parts, fmt.Sprintf("Skipped: %s", strings.Join(skipped, ", ")))
	}
	if len(parts) == 0 {
		b.sendText(chatID, "No new symbols were added. Check that the image shows valid tickers (e.g. AAPL, 005930.KS).")
		return
	}
	b.sendText(chatID, strings.Join(parts, "\n"))
	// If any were skipped due to limit and user has 10 slots, offer expansion
	limitReached := false
	for _, s := range skipped {
		if strings.Contains(s, "(limit reached)") {
			limitReached = true
			break
		}
	}
	if limitReached && b.getSlotLimit(chatID) == 10 {
		b.sendText(chatID, "Expand to 20 slots for 50 Stars/month — tap below to pay.")
		b.sendSlotsExpandInvoice(chatID)
	}
	if len(added) > 0 {
		b.sendOnboardingKeyboard(chatID, added)
	}
}

// sendOnboardingKeyboard sends "What would you like to do next?" with inline buttons.
func (b *Bot) sendOnboardingKeyboard(chatID int64, symbols []string) {
	symStr := ""
	if len(symbols) > 0 {
		symStr = " " + strings.Join(symbols, ", ")
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Set price alerts", "onboard:alerts"),
			tgbotapi.NewInlineKeyboardButtonData("Digest frequency", "onboard:digest"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Earnings reminders", "onboard:earnings"),
		),
	)
	payload := fmt.Sprintf("Added%s. What would you like to do next?", symStr)
	msg := tgbotapi.NewMessage(chatID, payload)
	msg.ReplyMarkup = kb
	b.API.Send(msg)
}

// doAdd performs the add-symbol logic (used by handleAdd and handlePendingSymbol).
func (b *Bot) doAdd(chatID int64, symbol string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	existing := b.Store.GetSymbols(chatID)
	slotLimit := b.getSlotLimit(chatID)
	if len(existing) >= slotLimit {
		if slotLimit == 10 {
			b.sendText(chatID, "You've reached the 10 stock limit. Expand to 20 slots for 50 Stars/month — tap below to pay.")
			b.sendSlotsExpandInvoice(chatID)
		} else {
			b.sendText(chatID, "You've reached the 20 stock limit.")
		}
		return
	}
	for _, s := range existing {
		if s == symbol {
			b.sendText(chatID, fmt.Sprintf("%s is already in your watchlist.", symbol))
			return
		}
	}

	b.sendText(chatID, fmt.Sprintf("Checking %s...", symbol))

	if err := yahoo.ValidateSymbol(symbol); err != nil {
		b.sendText(chatID, fmt.Sprintf("Symbol %s not found. Please check the ticker and try again.", symbol))
		log.Printf("Symbol validation failed for %s: %v", symbol, err)
		return
	}

	if err := b.Store.AddSymbol(chatID, symbol); err != nil {
		b.sendText(chatID, fmt.Sprintf("Could not add %s: %s", symbol, err.Error()))
		return
	}

	b.sendOnboardingKeyboard(chatID, []string{symbol})
}

// handleRemove removes a stock symbol from the user's watchlist.
// If no symbol is provided (e.g. user clicked /remove in menu), prompts them to reply with one.
func (b *Bot) handleRemove(msg *tgbotapi.Message) {
	symbol := strings.TrimSpace(msg.CommandArguments())
	if symbol == "" {
		b.pendingMu.Lock()
		b.pendingAction[msg.Chat.ID] = pendingRemove
		b.pendingMu.Unlock()
		b.sendText(msg.Chat.ID, "Which stock would you like to remove? Reply with the ticker (e.g. AAPL).")
		return
	}
	b.doRemove(msg.Chat.ID, symbol)
}

// doRemove performs the remove-symbol logic (used by handleRemove and handlePendingSymbol).
func (b *Bot) doRemove(chatID int64, symbol string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if err := b.Store.RemoveSymbol(chatID, symbol); err != nil {
		b.sendText(chatID, err.Error())
		return
	}

	b.sendText(chatID, fmt.Sprintf("Removed %s from your watchlist.", symbol))
}

// handlePendingSymbol processes a reply after commands that request follow-up input (/add, /remove, /news, /price, /alerts, /analyse).
func (b *Bot) handlePendingSymbol(chatID, userID int64, action, text string) {
	switch action {
	case pendingAdd:
		if text == "" {
			b.sendText(chatID, "Please send a ticker symbol (e.g. AAPL, MSFT).")
			return
		}
		b.doAdd(chatID, text)
	case pendingRemove:
		if text == "" {
			b.sendText(chatID, "Please send a ticker symbol (e.g. AAPL).")
			return
		}
		b.doRemove(chatID, text)
	case pendingNews:
		b.doNews(chatID, text)
	case pendingPrice:
		b.doPrice(chatID, text)
	case pendingAlerts:
		b.handleAddPriceAlert(chatID, text)
	case pendingDND:
		b.handleSetDND(chatID, text)
	case pendingAnalyse:
		if userID == 0 {
			userID = chatID // fallback for rate limit
		}
		allowed, _ := b.getAnalyseLimitStatus(userID)
		if !allowed {
			b.sendText(chatID, "You've used your 5 free analyses for today. Resets at midnight UTC.")
			return
		}
		b.doAnalyse(chatID, userID, text)
	}
}

// handleList shows all symbols in the user's watchlist.
func (b *Bot) handleList(msg *tgbotapi.Message) {
	symbols := b.Store.GetSymbols(msg.Chat.ID)
	if len(symbols) == 0 {
		b.sendText(msg.Chat.ID, "Your watchlist is empty. Use /add SYMBOL to add stocks.")
		return
	}

	text := "Your watchlist:\n"
	for i, s := range symbols {
		text += fmt.Sprintf("%d. %s\n", i+1, s)
	}
	b.sendText(msg.Chat.ID, text)
}

// sendSymbolOrWatchlistKeyboard sends a message with inline buttons [Symbol] and [Watchlist].
// prefix is used for callback data: prefix:symbol, prefix:watchlist.
func (b *Bot) sendSymbolOrWatchlistKeyboard(chatID int64, prompt, prefix string) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Symbol", prefix+":symbol"),
			tgbotapi.NewInlineKeyboardButtonData("Watchlist", prefix+":watchlist"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = kb
	if _, err := b.API.Send(msg); err != nil {
		log.Printf("sendSymbolOrWatchlistKeyboard: %v", err)
	}
}

// handleNews returns news articles. Supports:
//   - /news        -> inline keyboard: Symbol | Watchlist
//   - /news SYMBOL -> fetch live news for that symbol
func (b *Bot) handleNews(msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())

	if arg == "" {
		b.sendSymbolOrWatchlistKeyboard(msg.Chat.ID, "Which stock?", "news")
		return
	}

	b.doNews(msg.Chat.ID, arg)
}

// doNews fetches news for a symbol (or "all" for watchlist). Used by handleNews and handlePendingSymbol.
func (b *Bot) doNews(chatID int64, arg string) {
	arg = strings.TrimSpace(arg)
	useAll := arg == "" || strings.EqualFold(arg, "all") || strings.EqualFold(arg, "watchlist")

	if !useAll {
		symbol := strings.ToUpper(arg)
		b.sendText(chatID, fmt.Sprintf("Fetching news for %s...", symbol))

		articles, err := yahoo.FetchNews(symbol)
		if err != nil {
			b.sendText(chatID, fmt.Sprintf("Could not fetch news for %s. Please check the symbol and try again.", symbol))
			log.Printf("Live news fetch error for %s: %v", symbol, err)
			return
		}

		if len(articles) == 0 {
			b.sendText(chatID, fmt.Sprintf("No recent news found for %s.", symbol))
			return
		}

		// Cap at 10
		if len(articles) > 10 {
			articles = articles[:10]
		}

		text := fmt.Sprintf("<b>Latest news for %s</b>\n\n", symbol)
		for i, a := range articles {
			meta := a.Published.Format("Jan 02, 3:04 PM UTC")
			if a.Source != "" {
				meta = a.Source + " — " + meta
			}
			text += fmt.Sprintf("%d. <a href=\"%s\">%s</a>\n   <i>%s</i>\n\n",
				i+1,
				a.Link,
				escapeHTML(a.Title),
				escapeHTML(meta),
			)
		}
		b.SendHTML(chatID, text)
		return
	}

	// "all" — show cached news across watchlist
	symbols := b.Store.GetSymbols(chatID)
	if len(symbols) == 0 {
		b.sendText(chatID, "Your watchlist is empty. Use /add to add stocks, or reply with a symbol to look up any stock.")
		return
	}

	articles := b.Store.GetLatestArticles(chatID, "", 10)

	if len(articles) == 0 {
		b.sendText(chatID, "No cached news yet. News is fetched every hour — try again shortly.")
		return
	}

	text := "<b>Latest news for your watchlist</b>\n\n"
	for i, a := range articles {
		text += fmt.Sprintf("%d. <a href=\"%s\">%s</a>\n   <i>%s — %s</i>\n\n",
			i+1,
			a.Link,
			escapeHTML(a.Title),
			a.Symbol,
			a.Published.Format("Jan 02, 3:04 PM UTC"),
		)
	}

	b.SendHTML(chatID, text)
}

// handlePrice returns live price data. Supports:
//   - /price        -> inline keyboard: Symbol | Watchlist
//   - /price SYMBOL -> full trade-app style (regular, pre/post mkt, range, volume)
func (b *Bot) handlePrice(msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())

	if arg == "" {
		b.sendSymbolOrWatchlistKeyboard(msg.Chat.ID, "Which stock?", "price")
		return
	}

	b.doPrice(msg.Chat.ID, arg)
}

// doPrice fetches and displays price for a symbol or all watchlist symbols.
func (b *Bot) doPrice(chatID int64, arg string) {
	arg = strings.TrimSpace(arg)
	useAll := arg == "" || strings.EqualFold(arg, "all") || strings.EqualFold(arg, "watchlist")

	if !useAll {
		symbol := strings.ToUpper(arg)
		b.sendTyping(chatID)

		q, err := yahoo.FetchQuoteExtendedWithFallback(symbol)
		if err != nil {
			b.SendHTML(chatID, fmt.Sprintf("Could not fetch price for <b>%s</b>. Check the symbol and try again.", symbol))
			log.Printf("FetchQuoteExtended %s: %v", symbol, err)
			return
		}

		msg := formatPriceSingle(q)
		b.SendHTML(chatID, msg)
		return
	}

	// "all" — compact snapshot for watchlist
	symbols := b.Store.GetSymbols(chatID)
	if len(symbols) == 0 {
		b.sendText(chatID, "Your watchlist is empty. Use /add to add stocks, or reply with a symbol.")
		return
	}

	b.sendTyping(chatID)
	var lines []string
	for _, symbol := range symbols {
		q, err := yahoo.FetchQuoteExtendedWithFallback(symbol)
		if err != nil {
			lines = append(lines, fmt.Sprintf("• <b>%s</b>: —", symbol))
			log.Printf("FetchQuoteExtended %s: %v", symbol, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		lines = append(lines, fmt.Sprintf("• <b>%s</b> %s", symbol, q.SessionPriceSummary()))
		time.Sleep(300 * time.Millisecond)
	}

	text := "<b>Price snapshot</b>\n\n" + strings.Join(lines, "\n")
	b.SendHTML(chatID, text)
}

// formatPriceSingle formats extended quote as trade-app style message.
func formatPriceSingle(q *yahoo.QuoteExtended) string {
	pctStr := fmt.Sprintf("%+.1f%%", q.ChangePercent)
	volStr := formatVolumeCompact(q.Volume)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>  $%.2f  <b>%s</b>\n\n", q.Symbol, q.RegularMarketPrice, pctStr))
	sb.WriteString(fmt.Sprintf("Regular session: $%.2f (%s)  |  Prev close $%.2f\n", q.RegularMarketPrice, pctStr, q.PreviousClose))
	if q.DayHigh > 0 && q.DayLow > 0 {
		sb.WriteString(fmt.Sprintf("Day range: $%.2f – $%.2f  |  Vol: %s\n", q.DayLow, q.DayHigh, volStr))
	} else {
		sb.WriteString(fmt.Sprintf("Vol: %s\n", volStr))
	}

	if q.PreMarketPrice > 0 {
		if q.PreviousClose > 0 {
			pmPct := (q.PreMarketPrice - q.PreviousClose) / q.PreviousClose * 100
			sb.WriteString(fmt.Sprintf("\nPre-market: $%.2f (%+.1f%%)\n", q.PreMarketPrice, pmPct))
		} else {
			sb.WriteString(fmt.Sprintf("\nPre-market: $%.2f\n", q.PreMarketPrice))
		}
	}
	if q.PostMarketPrice > 0 {
		if q.PreviousClose > 0 {
			ahPct := (q.PostMarketPrice - q.PreviousClose) / q.PreviousClose * 100
			sb.WriteString(fmt.Sprintf("Post-market: $%.2f (%+.1f%%)\n", q.PostMarketPrice, ahPct))
		} else {
			sb.WriteString(fmt.Sprintf("Post-market: $%.2f\n", q.PostMarketPrice))
		}
	}

	return sb.String()
}

// formatVolumeCompact returns human-readable volume (e.g. "45.2M", "1.2K").
func formatVolumeCompact(v int64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(v)/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(v)/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", float64(v)/1_000)
	default:
		return fmt.Sprintf("%d", v)
	}
}

// handleReports shows upcoming earnings dates for the watchlist.
func (b *Bot) handleReports(msg *tgbotapi.Message) {
	b.handleReportsUpcoming(msg.Chat.ID)
}

// handleAlerts shows price alerts and lets user add/remove.
func (b *Bot) handleAlerts(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	arg := strings.TrimSpace(msg.CommandArguments())

	if arg != "" {
		// Try to parse "AAPL 5%" or "AAPL 150"
		b.handleAddPriceAlert(chatID, arg)
		return
	}

	alerts, err := b.Store.GetPriceAlerts(chatID)
	if err != nil {
		b.sendText(chatID, "Could not load alerts.")
		return
	}

	if len(alerts) == 0 {
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Add alert", "alerts:add"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Back to settings", "alerts:back"),
			),
		)
		msg := tgbotapi.NewMessage(chatID, "No price alerts set.\n\nAdd one: <code>/alerts AAPL 5%</code> or <code>/alerts AAPL 150</code> (price level)")
		msg.ReplyMarkup = kb
		msg.ParseMode = tgbotapi.ModeHTML
		b.API.Send(msg)
		return
	}

	var sb strings.Builder
	sb.WriteString("<b>Your price alerts</b>\n\n")
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, a := range alerts {
		desc := ""
		if a.Type == "pct" {
			desc = fmt.Sprintf("%s ±%.0f%%", a.Symbol, a.Threshold)
		} else {
			desc = fmt.Sprintf("%s $%.0f", a.Symbol, a.Threshold)
		}
		sb.WriteString(fmt.Sprintf("• %s\n", desc))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Remove "+a.Symbol+" "+a.Type, "alerts:remove:"+a.Symbol+":"+a.Type),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Add alert", "alerts:add"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Back to settings", "alerts:back"),
	))

	bm := tgbotapi.NewMessage(chatID, sb.String())
	bm.ParseMode = tgbotapi.ModeHTML
	bm.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.API.Send(bm)
}

// handleAddPriceAlert parses "AAPL 5%" or "AAPL 150" and adds the alert.
func (b *Bot) handleAddPriceAlert(chatID int64, arg string) {
	parts := strings.Fields(arg)
	if len(parts) < 2 {
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingAlerts
		b.pendingMu.Unlock()
		b.sendText(chatID, "Usage: /alerts SYMBOL THRESHOLD\nExamples:\n• /alerts AAPL 5% — alert when AAPL moves ±5%\n• /alerts AAPL 150 — alert when AAPL hits $150")
		return
	}
	symbol := strings.ToUpper(parts[0])
	valStr := parts[1]
	valStr = strings.TrimSuffix(valStr, "%")

	threshold, err := strconv.ParseFloat(valStr, 64)
	if err != nil || threshold <= 0 {
		b.sendText(chatID, "Invalid threshold. Use a number (e.g. 5 for 5%, or 150 for $150).")
		return
	}

	alertType := "price"
	if strings.Contains(parts[1], "%") {
		alertType = "pct"
	}

	if err := yahoo.ValidateSymbol(symbol); err != nil {
		b.sendText(chatID, fmt.Sprintf("Symbol %s not found.", symbol))
		return
	}

	if err := b.Store.AddPriceAlert(chatID, symbol, alertType, threshold); err != nil {
		b.sendText(chatID, "Could not add alert.")
		return
	}

	desc := ""
	if alertType == "pct" {
		desc = fmt.Sprintf("%s ±%.0f%%", symbol, threshold)
	} else {
		desc = fmt.Sprintf("%s $%.0f", symbol, threshold)
	}
	b.sendText(chatID, fmt.Sprintf("Alert added: %s. You'll get notified with news when it triggers.", desc))
}

// handleSetDND parses "HH:MM-HH:MM" (UTC) and sets DND window.
func (b *Bot) handleSetDND(chatID int64, text string) {
	text = strings.TrimSpace(text)
	parts := strings.Split(text, "-")
	if len(parts) != 2 {
		b.sendText(chatID, "Format: HH:MM-HH:MM (e.g. 22:00-07:00)")
		return
	}
	parseTime := func(s string) (int, int, bool) {
		s = strings.TrimSpace(s)
		seps := strings.Split(s, ":")
		if len(seps) != 2 {
			return 0, 0, false
		}
		h, eh := strconv.Atoi(strings.TrimSpace(seps[0]))
		m, em := strconv.Atoi(strings.TrimSpace(seps[1]))
		if eh != nil || em != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, 0, false
		}
		return h, m, true
	}
	h1, m1, ok1 := parseTime(parts[0])
	h2, m2, ok2 := parseTime(parts[1])
	if !ok1 || !ok2 {
		b.sendText(chatID, "Invalid time. Use HH:MM-HH:MM (e.g. 22:00-07:00)")
		return
	}
	start := time.Date(1970, 1, 1, h1, m1, 0, 0, time.UTC)
	end := time.Date(1970, 1, 1, h2, m2, 0, 0, time.UTC)
	prefs, _ := b.Store.GetPreferences(chatID)
	freq := 4
	if prefs != nil {
		freq = prefs.DigestFrequencyHours
	}
	if err := b.Store.SetPreferences(chatID, freq, &start, &end, ""); err != nil {
		b.sendText(chatID, "Could not set DND.")
		return
	}
	b.sendText(chatID, fmt.Sprintf("DND set: %02d:%02d - %02d:%02d UTC", h1, m1, h2, m2))
}

// handleSettings is an alias for handleConfigure.
func (b *Bot) handleSettings(msg *tgbotapi.Message) {
	b.handleConfigure(msg)
}

// handleConfigure shows a unified settings menu: digest frequency, alert frequency, DND, and manage alerts.
func (b *Bot) handleConfigure(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	prefs, _ := b.Store.GetPreferences(chatID)
	freq := 4
	alertFreq := 2
	dndStr := "Off"
	if prefs != nil {
		freq = prefs.DigestFrequencyHours
		if prefs.AlertFrequencyHours > 0 {
			alertFreq = prefs.AlertFrequencyHours
		}
		if prefs.DNDStartUTC != nil && prefs.DNDEndUTC != nil {
			dndStr = fmt.Sprintf("%02d:%02d - %02d:%02d UTC",
				prefs.DNDStartUTC.Hour(), prefs.DNDStartUTC.Minute(),
				prefs.DNDEndUTC.Hour(), prefs.DNDEndUTC.Minute())
		}
	}

	// Count active price alerts for display.
	alertCount := 0
	if alerts, err := b.Store.GetPriceAlerts(chatID); err == nil {
		alertCount = len(alerts)
	}

	alertLabel := fmt.Sprintf("Manage price alerts (%d active)", alertCount)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("2h", "settings:digest:2"),
			tgbotapi.NewInlineKeyboardButtonData("4h", "settings:digest:4"),
			tgbotapi.NewInlineKeyboardButtonData("8h", "settings:digest:8"),
			tgbotapi.NewInlineKeyboardButtonData("24h", "settings:digest:24"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1h", "settings:alert:1"),
			tgbotapi.NewInlineKeyboardButtonData("2h", "settings:alert:2"),
			tgbotapi.NewInlineKeyboardButtonData("4h", "settings:alert:4"),
			tgbotapi.NewInlineKeyboardButtonData("8h", "settings:alert:8"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(alertLabel, "settings:manage_alerts"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Set DND", "settings:dnd"),
			tgbotapi.NewInlineKeyboardButtonData("Clear DND", "settings:dnd:clear"),
		),
	)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("News digest: every %dh\n", freq))
	sb.WriteString(fmt.Sprintf("Price alerts: check every %dh (%d active)\n", alertFreq, alertCount))
	sb.WriteString(fmt.Sprintf("DND: %s\n", dndStr))
	sb.WriteString("\nTap to change:")

	msgOut := tgbotapi.NewMessage(chatID, sb.String())
	msgOut.ReplyMarkup = kb
	b.API.Send(msgOut)
}

// handleCallbackQuery handles inline keyboard button presses.
func (b *Bot) handleCallbackQuery(cq *tgbotapi.CallbackQuery) {
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.Chat.ID
	userID := int64(0)
	if cq.From != nil {
		userID = cq.From.ID
	}
	data := cq.Data

	cfg := tgbotapi.NewCallback(cq.ID, "")
	if _, err := b.API.Request(cfg); err != nil {
		log.Printf("AnswerCallbackQuery error: %v", err)
	}

	if b.requireEligible(chatID) {
		return
	}

	parts := strings.SplitN(data, ":", 2)
	switch {
	case len(parts) >= 2 && parts[0] == "help" && parts[1] == "back":
		// Edit message back to help overview.
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, helpOverviewText)
		kb := helpKeyboard()
		edit.ReplyMarkup = &kb
		b.API.Send(edit)
	case len(parts) >= 2 && parts[0] == "help":
		section, ok := helpSections[parts[1]]
		if !ok {
			return
		}
		backKb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("« Back", "help:back"),
			),
		)
		edit := tgbotapi.NewEditMessageText(chatID, cq.Message.MessageID, section)
		edit.ParseMode = tgbotapi.ModeHTML
		edit.ReplyMarkup = &backKb
		b.API.Send(edit)
	case len(parts) >= 2 && parts[0] == "reports" && parts[1] == "upcoming":
		b.handleReportsUpcoming(chatID)
	case len(parts) >= 2 && strings.HasPrefix(parts[1], "remind:"):
		sub := strings.TrimPrefix(parts[1], "remind:")
		subParts := strings.Split(sub, ":")
		if len(subParts) >= 2 {
			sym := subParts[0]
			enable := subParts[1] == "1"
			_ = b.Store.SetEarningsReminder(chatID, sym, enable)
			status := "Reminder on"
			if !enable {
				status = "Reminder off"
			}
			b.sendText(chatID, fmt.Sprintf("%s for %s.", status, sym))
			b.handleReportsUpcoming(chatID)
		}
	case len(parts) >= 2 && parts[0] == "news" && parts[1] == "watchlist":
		b.doNews(chatID, "all")
	case len(parts) >= 2 && parts[0] == "news" && parts[1] == "symbol":
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingNews
		b.pendingUserID[chatID] = userID
		b.pendingMu.Unlock()
		b.sendText(chatID, "Type the symbol (e.g. AAPL).")
	case len(parts) >= 2 && parts[0] == "price" && parts[1] == "watchlist":
		b.doPrice(chatID, "all")
	case len(parts) >= 2 && parts[0] == "price" && parts[1] == "symbol":
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingPrice
		b.pendingUserID[chatID] = userID
		b.pendingMu.Unlock()
		b.sendText(chatID, "Type the symbol (e.g. AAPL).")
	case len(parts) >= 2 && parts[0] == "analyse" && parts[1] == "watchlist":
		allowed, _ := b.getAnalyseLimitStatus(userID)
		if !allowed {
			b.sendText(chatID, "You've used your 5 free analyses for today. Resets at midnight UTC.")
			return
		}
		b.doAnalyse(chatID, userID, "all")
	case len(parts) >= 2 && parts[0] == "analyse" && parts[1] == "symbol":
		// Limit is checked when user submits symbol in handlePendingSymbol
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingAnalyse
		b.pendingUserID[chatID] = userID
		b.pendingMu.Unlock()
		b.sendText(chatID, "Type the symbol (e.g. AAPL).")
	case len(parts) >= 2 && parts[0] == "alerts" && parts[1] == "back":
		b.handleConfigure(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
	case len(parts) >= 2 && parts[0] == "alerts" && parts[1] == "add":
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingAlerts
		b.pendingUserID[chatID] = userID
		b.pendingMu.Unlock()
		b.SendHTML(chatID, "Type symbol and threshold, e.g. <code>AAPL 5%</code> or <code>AAPL 150</code>")
	case len(parts) >= 2 && strings.HasPrefix(parts[1], "remove:"):
		sub := strings.TrimPrefix(parts[1], "remove:")
		subParts := strings.Split(sub, ":")
		if len(subParts) >= 2 {
			sym, atype := subParts[0], subParts[1]
			if err := b.Store.RemovePriceAlert(chatID, sym, atype); err != nil {
				b.sendText(chatID, "Could not remove alert.")
			} else {
				b.sendText(chatID, fmt.Sprintf("Removed %s %s alert.", sym, atype))
			}
			b.handleAlerts(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
		}
	case len(parts) >= 2 && strings.HasPrefix(parts[1], "digest:"):
		sub := strings.TrimPrefix(parts[1], "digest:")
		if h, err := strconv.Atoi(sub); err == nil && (h == 2 || h == 4 || h == 8 || h == 24) {
			_ = b.Store.SetDigestFrequency(chatID, h)
			b.sendText(chatID, fmt.Sprintf("Digest frequency set to every %d hours.", h))
		}
	case len(parts) >= 2 && strings.HasPrefix(parts[1], "alert:"):
		sub := strings.TrimPrefix(parts[1], "alert:")
		if h, err := strconv.Atoi(sub); err == nil && (h == 1 || h == 2 || h == 4 || h == 8) {
			_ = b.Store.SetAlertFrequency(chatID, h)
			b.sendText(chatID, fmt.Sprintf("Price alert frequency set to every %d hour(s).", h))
		}
	case len(parts) >= 2 && parts[1] == "manage_alerts":
		b.handleAlerts(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
	case len(parts) >= 2 && parts[1] == "dnd":
		b.pendingMu.Lock()
		b.pendingAction[chatID] = pendingDND
		b.pendingUserID[chatID] = userID
		b.pendingMu.Unlock()
		b.SendHTML(chatID, "Reply with DND window in UTC, e.g. <code>22:00-07:00</code> (10 PM to 7 AM). No digests or alerts will be sent during this time.")
	case len(parts) >= 2 && parts[0] == "onboard":
		switch parts[1] {
		case "alerts":
			b.handleAlerts(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
		case "digest":
			b.handleSettings(&tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
		case "earnings":
			b.handleReportsUpcoming(chatID)
			b.sendText(chatID, "Use /reports to see earnings. Toggle reminders per stock there.")
		}
	case len(parts) >= 2 && parts[1] == "dnd:clear":
		prefs, _ := b.Store.GetPreferences(chatID)
		freq := 4
		if prefs != nil {
			freq = prefs.DigestFrequencyHours
		}
		_ = b.Store.SetPreferences(chatID, freq, nil, nil, "")
		b.sendText(chatID, "DND cleared.")
	default:
		b.sendText(chatID, "Unknown action.")
	}
}

// handleReportsUpcoming shows upcoming earnings: "This week" and "Later", with reminder toggles.
func (b *Bot) handleReportsUpcoming(chatID int64) {
	symbols := b.Store.GetSymbols(chatID)
	if len(symbols) == 0 {
		b.sendText(chatID, "Your watchlist is empty.")
		return
	}

	b.sendTyping(chatID)
	now := time.Now().UTC()
	weekEnd := now.Add(7 * 24 * time.Hour)
	var thisWeek, later []struct {
		symbol  string
		date    time.Time
		quarter string
	}

	for _, symbol := range symbols {
		info, err := yahoo.FetchEarnings(symbol)
		if err != nil {
			log.Printf("Earnings fetch error for %s: %v", symbol, err)
			continue
		}
		ed := time.Date(info.EarningsDate.Year(), info.EarningsDate.Month(), info.EarningsDate.Day(), 0, 0, 0, 0, time.UTC)
		entry := struct {
			symbol  string
			date    time.Time
			quarter string
		}{symbol, ed, info.Quarter}
		if !ed.Before(now) && ed.Before(weekEnd) {
			thisWeek = append(thisWeek, entry)
		} else if !ed.Before(weekEnd) {
			later = append(later, entry)
		}
		time.Sleep(200 * time.Millisecond)
	}

	var sb strings.Builder
	sb.WriteString("<b>Upcoming Earnings Reports</b>\n\n")
	var rows [][]tgbotapi.InlineKeyboardButton

	if len(thisWeek) > 0 {
		sb.WriteString("<b>This week</b>\n")
		for _, e := range thisWeek {
			enabled, _ := b.Store.IsEarningsReminderEnabled(chatID, e.symbol)
			remindLabel := "Don't remind"
			remindVal := "0"
			if !enabled {
				remindLabel = "Remind me"
				remindVal = "1"
			}
			sb.WriteString(fmt.Sprintf("• <b>%s</b>: %s (%s) [%s]\n",
				e.symbol, e.date.Format("Jan 02, 2006"), e.quarter, map[bool]string{true: "reminder on", false: "reminder off"}[enabled]))
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(remindLabel+" "+e.symbol, "reports:remind:"+e.symbol+":"+remindVal),
			))
		}
		sb.WriteString("\n")
	}
	if len(later) > 0 {
		sb.WriteString("<b>Later</b>\n")
		for _, e := range later {
			enabled, _ := b.Store.IsEarningsReminderEnabled(chatID, e.symbol)
			remindLabel := "Don't remind"
			remindVal := "0"
			if !enabled {
				remindLabel = "Remind me"
				remindVal = "1"
			}
			sb.WriteString(fmt.Sprintf("• <b>%s</b>: %s (%s) [%s]\n",
				e.symbol, e.date.Format("Jan 02, 2006"), e.quarter, map[bool]string{true: "reminder on", false: "reminder off"}[enabled]))
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(remindLabel+" "+e.symbol, "reports:remind:"+e.symbol+":"+remindVal),
			))
		}
	}

	if len(thisWeek) == 0 && len(later) == 0 {
		sb.WriteString("No upcoming earnings dates found for your watchlist.")
	}

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = tgbotapi.ModeHTML
	if len(rows) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}
	b.API.Send(msg)
}

// handleAnalyse generates an AI-powered stock analysis. Supports:
//   - /analyse        -> inline keyboard: Symbol | Watchlist
//   - /analyse SYMBOL -> analyse a specific stock
func (b *Bot) handleAnalyse(msg *tgbotapi.Message) {
	// Check daily usage limit (read-only; slot consumed in doAnalyse)
	allowed, _ := b.getAnalyseLimitStatus(msg.From.ID)
	if !allowed {
		b.sendText(msg.Chat.ID, "You've used your 5 free analyses for today. Resets at midnight UTC.")
		return
	}

	arg := strings.TrimSpace(msg.CommandArguments())

	if arg == "" {
		b.sendSymbolOrWatchlistKeyboard(msg.Chat.ID, "Which stock?", "analyse")
		return
	}

	b.doAnalyse(msg.Chat.ID, msg.From.ID, arg)
}

// doAnalyse runs analysis for a symbol or "all" watchlist. Used by handleAnalyse and handlePendingSymbol.
func (b *Bot) doAnalyse(chatID, userID int64, arg string) {
	arg = strings.TrimSpace(arg)
	useAll := arg == "" || strings.EqualFold(arg, "all") || strings.EqualFold(arg, "watchlist")

	var symbols []string
	if useAll {
		symbols = b.Store.GetSymbols(chatID)
		if len(symbols) == 0 {
			b.sendText(chatID, "Your watchlist is empty. Use /add to add stocks first.")
			return
		}
	} else {
		symbols = []string{strings.ToUpper(arg)}
	}

	allowed, remaining := b.consumeAnalyseSlot(userID)
	if !allowed {
		b.sendText(chatID, "You've used your 5 free analyses for today. Resets at midnight UTC.")
		return
	}

	for i, symbol := range symbols {
		b.sendText(chatID, fmt.Sprintf("Analysing %s... (this may take a moment)", symbol))

		// Gather data: cached news, live price, earnings
		articles := b.Store.GetLatestArticles(chatID, symbol, 15)

		quote, err := yahoo.FetchQuote(symbol)
		if err != nil {
			log.Printf("Quote fetch error for %s: %v", symbol, err)
			// Continue with nil quote — analysis can still work with just news
		}

		earnings, err := yahoo.FetchEarnings(symbol)
		if err != nil {
			log.Printf("Earnings fetch error for %s: %v", symbol, err)
		}

		technicals, _ := yahoo.ComputeTechnicals(symbol)
		if technicals == nil {
			time.Sleep(200 * time.Millisecond)
		}

		result, err := b.Analyser.Analyse(symbol, articles, quote, earnings, technicals)
		if err != nil {
			b.sendText(chatID, fmt.Sprintf("Could not generate analysis for %s: %v", symbol, err))
			log.Printf("Analysis error for %s: %v", symbol, err)
			continue
		}

		// Send as plain text (the AI is instructed not to use markup)
		header := fmt.Sprintf("=== Analysis: %s ===\n\n", symbol)
		footer := ""
		if i == len(symbols)-1 {
			footer = fmt.Sprintf("\n\n(%d analyses remaining today)", remaining)
		}
		b.sendText(chatID, header+result+footer)
	}
}

// escapeHTML escapes special characters for Telegram HTML parse mode.
func escapeHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}
