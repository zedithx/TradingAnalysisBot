package bot

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"TradingNewsBot/yahoo"
)

// handleStart sends a welcome message.
func (b *Bot) handleStart(msg *tgbotapi.Message) {
	text := `Welcome to TradingNewsBot!

Your personal market intelligence assistant. I help you stay on top of the stocks you care about by consolidating news, earnings data, and AI-driven analysis in one place.

-- HOW TO USE --

1. BUILD YOUR WATCHLIST
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

4. AI ANALYSIS
/analyse SYMBOL - AI-powered analysis for a specific stock
/analyse - AI analysis for all your watchlist stocks

Consolidates recent news, live price data, and earnings information, then uses AI to provide a sentiment summary, key risk factors, and a short-term outlook.

-- TIPS --
Use exchange suffixes for non-US stocks:
  005930.KS (Samsung, Korea)
  0700.HK (Tencent, Hong Kong)
  D05.SI (DBS, Singapore)

Type /help anytime to see the command list.

DISCLAIMER: This bot is for informational and educational purposes only. Nothing provided here constitutes financial advice, investment recommendations, or a solicitation to buy or sell any securities. The creators and operators of this bot are not licensed financial advisors and are not responsible for any investment decisions or losses incurred based on information provided. Always do your own research and consult a qualified financial professional before making any investment decisions.`

	b.sendText(msg.Chat.ID, text)
}

// handleHelp lists all available commands.
func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	text := `-- HOW TO USE --

1. BUILD YOUR WATCHLIST
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

4. AI ANALYSIS
/analyse SYMBOL - AI-powered analysis for a specific stock
/analyse - AI analysis for all your watchlist stocks

Consolidates recent news, live price data, and earnings information, then uses AI to provide a sentiment summary, key risk factors, and a short-term outlook.

-- TIPS --
Use exchange suffixes for non-US stocks:
  005930.KS (Samsung, Korea)
  0700.HK (Tencent, Hong Kong)
  D05.SI (DBS, Singapore)

DISCLAIMER: This bot is for informational and educational purposes only. Nothing provided here constitutes financial advice, investment recommendations, or a solicitation to buy or sell any securities. The creators and operators of this bot are not licensed financial advisors and are not responsible for any investment decisions or losses incurred based on information provided. Always do your own research and consult a qualified financial professional before making any investment decisions.`

	b.sendText(msg.Chat.ID, text)
}

// handleAdd validates and adds a stock symbol to the user's watchlist.
func (b *Bot) handleAdd(msg *tgbotapi.Message) {
	symbol := strings.TrimSpace(msg.CommandArguments())
	if symbol == "" {
		b.sendText(msg.Chat.ID, "Usage: /add SYMBOL\nExample: /add AAPL")
		return
	}
	symbol = strings.ToUpper(symbol)

	// Check for duplicate and watchlist limit (fast, no network call)
	existing := b.Store.GetSymbols(msg.Chat.ID)
	if len(existing) >= 10 {
		b.sendText(msg.Chat.ID, "You've reached the 10 stock limit. Please pay 5 dolla to 83055237 to unlock more slots.")
		return
	}
	for _, s := range existing {
		if s == symbol {
			b.sendText(msg.Chat.ID, fmt.Sprintf("%s is already in your watchlist.", symbol))
			return
		}
	}

	// Validate symbol with Yahoo Finance
	b.sendText(msg.Chat.ID, fmt.Sprintf("Checking %s...", symbol))

	if err := yahoo.ValidateSymbol(symbol); err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Symbol %s not found. Please check the ticker and try again.", symbol))
		log.Printf("Symbol validation failed for %s: %v", symbol, err)
		return
	}

	if err := b.Store.AddSymbol(msg.Chat.ID, symbol); err != nil {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Could not add %s: %s", symbol, err.Error()))
		return
	}

	b.sendText(msg.Chat.ID, fmt.Sprintf("Added %s to your watchlist. News will be fetched within the next hour.", symbol))
}

// handleRemove removes a stock symbol from the user's watchlist.
func (b *Bot) handleRemove(msg *tgbotapi.Message) {
	symbol := strings.TrimSpace(msg.CommandArguments())
	if symbol == "" {
		b.sendText(msg.Chat.ID, "Usage: /remove SYMBOL\nExample: /remove AAPL")
		return
	}
	symbol = strings.ToUpper(symbol)

	if err := b.Store.RemoveSymbol(msg.Chat.ID, symbol); err != nil {
		b.sendText(msg.Chat.ID, err.Error())
		return
	}

	b.sendText(msg.Chat.ID, fmt.Sprintf("Removed %s from your watchlist.", symbol))
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

// handleNews returns news articles. Supports:
//   - /news        -> last 10 cached articles across all watchlist symbols
//   - /news SYMBOL -> fetch live news for any symbol (doesn't need to be in watchlist)
func (b *Bot) handleNews(msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())

	// If a specific symbol is given, fetch live — no watchlist needed
	if arg != "" {
		symbol := strings.ToUpper(arg)
		b.sendText(msg.Chat.ID, fmt.Sprintf("Fetching news for %s...", symbol))

		articles, err := yahoo.FetchNews(symbol)
		if err != nil {
			b.sendText(msg.Chat.ID, fmt.Sprintf("Could not fetch news for %s. Please check the symbol and try again.", symbol))
			log.Printf("Live news fetch error for %s: %v", symbol, err)
			return
		}

		if len(articles) == 0 {
			b.sendText(msg.Chat.ID, fmt.Sprintf("No recent news found for %s.", symbol))
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
		b.SendHTML(msg.Chat.ID, text)
		return
	}

	// No argument — show cached news across watchlist
	symbols := b.Store.GetSymbols(msg.Chat.ID)
	if len(symbols) == 0 {
		b.sendText(msg.Chat.ID, "Your watchlist is empty. Use /add SYMBOL to add stocks, or use /news SYMBOL to look up any stock directly.")
		return
	}

	articles := b.Store.GetLatestArticles(msg.Chat.ID, "", 10)

	if len(articles) == 0 {
		b.sendText(msg.Chat.ID, "No cached news yet. News is fetched every hour — try again shortly.")
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

	b.SendHTML(msg.Chat.ID, text)
}

// handleReports shows next earnings dates for all watchlist symbols.
func (b *Bot) handleReports(msg *tgbotapi.Message) {
	symbols := b.Store.GetSymbols(msg.Chat.ID)
	if len(symbols) == 0 {
		b.sendText(msg.Chat.ID, "Your watchlist is empty. Use /add SYMBOL to add stocks first.")
		return
	}

	b.sendText(msg.Chat.ID, "Fetching earnings report dates...")

	text := "<b>Upcoming Earnings Reports</b>\n\n"
	for _, symbol := range symbols {
		info, err := yahoo.FetchEarnings(symbol)
		if err != nil {
			text += fmt.Sprintf("• <b>%s</b>: No earnings date available\n", symbol)
			log.Printf("Earnings fetch error for %s: %v", symbol, err)
			continue
		}

		text += fmt.Sprintf("• <b>%s</b>: %s (%s)\n",
			symbol,
			info.EarningsDate.Format("Jan 02, 2006"),
			info.Quarter,
		)
	}

	b.SendHTML(msg.Chat.ID, text)
}

// handleAnalyse generates an AI-powered stock analysis. Supports:
//   - /analyse        -> analyse each stock in the watchlist
//   - /analyse SYMBOL -> analyse a specific stock
func (b *Bot) handleAnalyse(msg *tgbotapi.Message) {
	// Check daily usage limit
	allowed, remaining := b.checkAnalyseLimit(msg.From.ID)
	if !allowed {
		b.sendText(msg.Chat.ID, "You've used your 2 free analyses for today. Resets at midnight UTC.")
		return
	}

	arg := strings.TrimSpace(msg.CommandArguments())

	var symbols []string
	if arg != "" {
		symbols = []string{strings.ToUpper(arg)}
	} else {
		symbols = b.Store.GetSymbols(msg.Chat.ID)
		if len(symbols) == 0 {
			b.sendText(msg.Chat.ID, "Your watchlist is empty. Use /add SYMBOL to add stocks first.")
			return
		}
	}

	for _, symbol := range symbols {
		b.sendText(msg.Chat.ID, fmt.Sprintf("Analysing %s... (this may take a moment)", symbol))

		// Gather data: cached news, live price, earnings
		articles := b.Store.GetLatestArticles(msg.Chat.ID, symbol, 15)

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
			b.sendText(msg.Chat.ID, fmt.Sprintf("Could not generate analysis for %s: %v", symbol, err))
			log.Printf("Analysis error for %s: %v", symbol, err)
			continue
		}

		// Send as plain text (the AI is instructed not to use markup)
		header := fmt.Sprintf("=== Analysis: %s ===\n\n", symbol)
		footer := fmt.Sprintf("\n\n(%d/%d analyses remaining today)", remaining, maxAnalysePerDay)
		b.sendText(msg.Chat.ID, header+result+footer)
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
