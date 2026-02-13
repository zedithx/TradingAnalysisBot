package analysis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"TradingNewsBot/storage"
	"TradingNewsBot/yahoo"
)

// Analyser generates AI-powered stock analysis using OpenAI.
type Analyser struct {
	client openai.Client
}

// New creates an Analyser with the given OpenAI API key.
func New(apiKey string) *Analyser {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Analyser{client: client}
}

// Analyse generates a market analysis for the given symbol using recent news and price data.
func (a *Analyser) Analyse(symbol string, articles []storage.CachedArticle, quote *yahoo.QuoteData, earnings *yahoo.EarningsInfo) (string, error) {
	prompt := buildPrompt(symbol, articles, quote, earnings)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	completion, err := a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return completion.Choices[0].Message.Content, nil
}

const systemPrompt = `You are a senior equity research analyst at Goldman Sachs with 20+ years of experience covering global markets. You think in terms of risk/reward, catalysts, positioning, and institutional flows. You've seen multiple market cycles and have a sharp nose for when consensus narrative diverges from reality.

Your analysis framework:

SENTIMENT & NARRATIVE
• Distil what the news flow signals about institutional and retail sentiment
• Identify if current narrative is priced in or if there's an expectation gap
• Note any shift in tone vs prior weeks (acceleration, deceleration, pivot)

FUNDAMENTAL DRIVERS
• Key operating metrics and what they imply (revenue trajectory, margin pressure/expansion, FCF)
• Where the company sits in its earnings cycle — pre/post report positioning
• Sector-level tailwinds or headwinds that affect the thesis

CATALYSTS & TIMELINE
• Near-term catalysts (earnings, product launches, regulatory decisions, macro data)
• Estimated timeframe and probability-weighted impact
• What the market is NOT pricing in that could move the stock

RISK/REWARD ASSESSMENT
• Asymmetry: is the risk/reward skewed favourably or unfavourably at current levels?
• Downside scenarios and their likelihood
• What would make you change your view (bull case invalidation / bear case invalidation)

POSITIONING TAKE
• Conclude with a clear, actionable view: accumulate, hold, trim, or avoid
• Specify conviction level (high / moderate / low) and time horizon

Rules:
• Be direct and opinionated — don't hedge every sentence. Take a stance.
• Use institutional language but keep it accessible. No jargon for jargon's sake.
• If the data is limited, say so — don't fabricate confidence.
• Keep response under 2500 characters for Telegram readability.
• Use plain text only — no markdown, no HTML, no bold/italic syntax.
• Use line breaks and bullets (•) for structure.
• End with a one-line disclaimer: this is AI-generated analysis, not financial advice.`

// buildPrompt constructs the user prompt with all available data for a stock.
func buildPrompt(symbol string, articles []storage.CachedArticle, quote *yahoo.QuoteData, earnings *yahoo.EarningsInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("COVERAGE REQUEST: %s\n", symbol))
	sb.WriteString("Generate a concise equity research note based on the following data.\n\n")

	// Price data
	if quote != nil {
		sb.WriteString("--- PRICE ACTION ---\n")
		sb.WriteString(fmt.Sprintf("Last: %.2f %s\n", quote.RegularMarketPrice, quote.Currency))
		sb.WriteString(fmt.Sprintf("Prev Close: %.2f\n", quote.PreviousClose))

		direction := "+"
		if quote.Change < 0 {
			direction = ""
		}
		sb.WriteString(fmt.Sprintf("Change: %s%.2f (%s%.2f%%)\n", direction, quote.Change, direction, quote.ChangePercent))

		if quote.DayHigh > 0 {
			spread := quote.DayHigh - quote.DayLow
			sb.WriteString(fmt.Sprintf("Intraday Range: %.2f – %.2f (spread: %.2f)\n", quote.DayLow, quote.DayHigh, spread))
		}
		if quote.Volume > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n", formatVolume(quote.Volume)))
		}
		sb.WriteString(fmt.Sprintf("Exchange: %s\n", quote.Exchange))
		sb.WriteString("\n")
	} else {
		sb.WriteString("--- PRICE ACTION ---\nNo live quote available.\n\n")
	}

	// Earnings
	if earnings != nil {
		daysUntil := int(time.Until(earnings.EarningsDate).Hours() / 24)
		sb.WriteString(fmt.Sprintf("--- EARNINGS ---\nNext Report: %s (%s) — %d days away\n\n",
			earnings.EarningsDate.Format("Jan 02, 2006"), earnings.Quarter, daysUntil))
	} else {
		sb.WriteString("--- EARNINGS ---\nNo upcoming earnings date available.\n\n")
	}

	// News headlines
	sb.WriteString("--- NEWS FLOW ---\n")
	if len(articles) > 0 {
		sb.WriteString(fmt.Sprintf("%d recent headlines (newest first):\n", min(len(articles), 15)))
		for i, a := range articles {
			if i >= 15 {
				break
			}
			age := time.Since(a.Published)
			ageStr := formatAge(age)
			sb.WriteString(fmt.Sprintf("%d. [%s ago] %s\n", i+1, ageStr, a.Title))
		}
	} else {
		sb.WriteString("No recent news flow — limited data environment. Note this in your analysis.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("Provide your research note on this name. Be direct about your view and conviction level.")

	return sb.String()
}

// formatVolume returns a human-readable volume string (e.g. "12.3M", "1.5B").
func formatVolume(v int64) string {
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

// formatAge returns a concise human-readable duration (e.g. "2h", "3d", "1w").
func formatAge(d time.Duration) string {
	hours := int(d.Hours())
	if hours < 1 {
		return "<1h"
	}
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	}
	days := hours / 24
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dw", days/7)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
