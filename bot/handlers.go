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

// handleSuccessfulPayment processes a successful payment and extends subscription.
func (b *Bot) handleSuccessfulPayment(chatID int64, payment *tgbotapi.SuccessfulPayment) {
	payload := payment.InvoicePayload
	if !strings.HasPrefix(payload, "sub:") {
		log.Printf("Unexpected invoice payload: %s", payload)
		return
	}
	idStr := strings.TrimPrefix(payload, "sub:")
	parsedChatID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		log.Printf("Parse payload chatID error: %v", err)
		parsedChatID = chatID
	}

	if err := b.Store.ExtendSubscription(parsedChatID); err != nil {
		log.Printf("SetSubscribedUntil error: %v", err)
		b.sendText(parsedChatID, "Payment received but there was an error activating your subscription. Contact me on Reddit: u/Logical-Albatross873")
		return
	}

	b.sendText(parsedChatID, "Thanks! Your subscription is active for 30 days. Use /start to get going.")
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

I'm here to help you keep tabs on the stocks you care about. Instead of jumping between apps and feeds, I bring together news, earnings dates, and AI analysis in one place.

You'll get news summaries every 4 hours, morning snapshots, price alerts for your watchlist, and a weekly recap — all sent automatically.

First 30 days are free, then 100 Stars/month.

Start by adding a few stocks: /add, then reply with a ticker (like AAPL) or send a screenshot of your watchlist — I'll pull the symbols out. You can add up to 10. /list shows what you're tracking.

Then: /news for headlines, /reports for earnings dates, /analyse for AI summaries. /help has the full list.

/support for payment help. (Info only — not financial advice.)`
	b.sendText(chatID, welcome)
}

// handleHelp lists all available commands.
func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	text := `-- SUBSCRIPTION --
First 30 days free, then 100 Stars/month. /support for payment questions.

-- HOW TO USE --

1. BUILD YOUR WATCHLIST
/add - Reply with ticker or send a photo of your watchlist
/add SYMBOL - Track a stock (e.g. /add AAPL)
/remove SYMBOL - Stop tracking a stock
/list - View your current watchlist

2. MONITOR NEWS & SENTIMENT
/news - Latest 10 headlines across your watchlist
/news SYMBOL - Latest 10 headlines for any stock

News headlines are one of the most important tools for gauging market sentiment. When significant negative news breaks (lawsuits, missed guidance, regulatory action), it often drives selling pressure. Conversely, positive catalysts (strong partnerships, product launches, analyst upgrades) can signal buying momentum. Use /news regularly to stay ahead of price-moving events.

3. TRACK EARNINGS DATES
/reports - Next earnings report dates for your watchlist

Earnings reports (Q1, Q2, Q3, Q4) are when companies publish their financial statements, including revenue, net income, expenses, and forward guidance. These are critical dates for any investor. Strong earnings can validate a company's growth trajectory, while misses can trigger sharp sell-offs. Knowing when reports are due helps you prepare for potential volatility and make informed decisions around those dates.

4. LIVE PRICE
/price SYMBOL - Live price, % change, pre/post market, day range, volume
/price - Reply with symbol or "all" for watchlist

5. AI ANALYSIS
/analyse SYMBOL - AI-powered analysis for a specific stock
/analyse - AI analysis for all your watchlist stocks

Consolidates recent news, live price data, and earnings information, then uses AI to provide a sentiment summary, key risk factors, and a short-term outlook.

-- TIPS --
Use exchange suffixes for non-US stocks:
  005930.KS (Samsung, Korea)
  0700.HK (Tencent, Hong Kong)
  D05.SI (DBS, Singapore)

/support — Payment or subscription issues
/terms — Terms and conditions

DISCLAIMER: This bot is for informational and educational purposes only. Nothing provided here constitutes financial advice, investment recommendations, or a solicitation to buy or sell any securities. The creators and operators of this bot are not licensed financial advisors and are not responsible for any investment decisions or losses incurred based on information provided. Always do your own research and consult a qualified financial professional before making any investment decisions.`

	b.sendText(msg.Chat.ID, text)
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
	slotLimit := 10
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
}

// doAdd performs the add-symbol logic (used by handleAdd and handlePendingSymbol).
func (b *Bot) doAdd(chatID int64, symbol string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	existing := b.Store.GetSymbols(chatID)
	if len(existing) >= 10 {
		b.sendText(chatID, "You've reached the 10 stock limit. Please pay 5 dolla to 83055237 to unlock more slots.")
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

	b.sendText(chatID, fmt.Sprintf("Added %s to your watchlist. News will be fetched within the next hour.", symbol))
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

// handlePendingSymbol processes a reply after /add, /remove, /news, or /analyse was sent without arguments.
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
	case pendingAnalyse:
		if userID == 0 {
			userID = chatID // fallback for rate limit
		}
		allowed, _ := b.checkAnalyseLimit(userID)
		if !allowed {
			b.sendText(chatID, "You've used your 2 free analyses for today. Resets at midnight UTC.")
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
		pctStr := fmt.Sprintf("%+.1f%%", q.ChangePercent)
		lines = append(lines, fmt.Sprintf("• <b>%s</b> $%.2f %s", symbol, q.RegularMarketPrice, pctStr))
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
	sb.WriteString(fmt.Sprintf("Regular: $%.2f (%s)  |  Prev close $%.2f\n", q.RegularMarketPrice, pctStr, q.PreviousClose))
	if q.DayHigh > 0 && q.DayLow > 0 {
		sb.WriteString(fmt.Sprintf("Day range: $%.2f – $%.2f  |  Vol: %s\n", q.DayLow, q.DayHigh, volStr))
	} else {
		sb.WriteString(fmt.Sprintf("Vol: %s\n", volStr))
	}

	if q.PreMarketPrice > 0 && q.PreviousClose > 0 {
		pmPct := (q.PreMarketPrice - q.PreviousClose) / q.PreviousClose * 100
		sb.WriteString(fmt.Sprintf("\nPre-mkt: $%.2f (%+.1f%%)\n", q.PreMarketPrice, pmPct))
	}
	if q.PostMarketPrice > 0 && q.PreviousClose > 0 {
		ahPct := (q.PostMarketPrice - q.PreviousClose) / q.PreviousClose * 100
		sb.WriteString(fmt.Sprintf("Post-mkt: $%.2f (%+.1f%%)\n", q.PostMarketPrice, ahPct))
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
	case len(parts) >= 2 && parts[0] == "reports" && parts[1] == "upcoming":
		b.handleReportsUpcoming(chatID)
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
		allowed, _ := b.checkAnalyseLimit(userID)
		if !allowed {
			b.sendText(chatID, "You've used your 2 free analyses for today. Resets at midnight UTC.")
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
	default:
		b.sendText(chatID, "Unknown action.")
	}
}

// handleReportsUpcoming shows upcoming earnings dates for watchlist.
func (b *Bot) handleReportsUpcoming(chatID int64) {
	symbols := b.Store.GetSymbols(chatID)
	if len(symbols) == 0 {
		b.sendText(chatID, "Your watchlist is empty.")
		return
	}

	b.sendTyping(chatID)
	text := "<b>Upcoming Earnings Reports</b>\n\n"
	for _, symbol := range symbols {
		info, err := yahoo.FetchEarnings(symbol)
		if err != nil {
			text += fmt.Sprintf("• <b>%s</b>: No earnings date available\n", symbol)
			log.Printf("Earnings fetch error for %s: %v", symbol, err)
			continue
		}
		text += fmt.Sprintf("• <b>%s</b>: %s (%s)\n",
			symbol, info.EarningsDate.Format("Jan 02, 2006"), info.Quarter)
	}
	b.SendHTML(chatID, text)
}

// handleAnalyse generates an AI-powered stock analysis. Supports:
//   - /analyse        -> inline keyboard: Symbol | Watchlist
//   - /analyse SYMBOL -> analyse a specific stock
func (b *Bot) handleAnalyse(msg *tgbotapi.Message) {
	// Check daily usage limit
	allowed, _ := b.checkAnalyseLimit(msg.From.ID)
	if !allowed {
		b.sendText(msg.Chat.ID, "You've used your 2 free analyses for today. Resets at midnight UTC.")
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

	allowed, remaining := b.checkAnalyseLimit(userID)
	if !allowed {
		b.sendText(chatID, "You've used your 2 free analyses for today. Resets at midnight UTC.")
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
			// Continue with nil earnings
		}

		result, err := b.Analyser.Analyse(symbol, articles, quote, earnings)
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
