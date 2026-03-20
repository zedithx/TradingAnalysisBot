package yahoo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// QuoteExtended holds extended price data for alerts (pre/post market, avg volume, etc).
type QuoteExtended struct {
	Symbol             string
	RegularMarketPrice float64
	PreviousClose      float64
	ChangePercent      float64
	DayHigh            float64
	DayLow             float64
	Volume             int64
	AverageVolume      int64
	PreMarketPrice     float64
	PostMarketPrice    float64
	RegularMarketOpen  float64
}

// SessionPriceSummary returns a compact regular/pre/post market price summary.
func (q *QuoteExtended) SessionPriceSummary() string {
	if q == nil {
		return "No live price"
	}

	parts := make([]string, 0, 3)
	if q.RegularMarketPrice > 0 {
		parts = append(parts, fmt.Sprintf("Regular $%.2f (%+.1f%%)", q.RegularMarketPrice, q.ChangePercent))
	}
	if pre := formatExtendedSessionPart("Pre-market", q.PreMarketPrice, q.PreviousClose); pre != "" {
		parts = append(parts, pre)
	}
	if post := formatExtendedSessionPart("Post-market", q.PostMarketPrice, q.PreviousClose); post != "" {
		parts = append(parts, post)
	}
	if len(parts) == 0 {
		return "No live price"
	}
	return strings.Join(parts, " | ")
}

func formatExtendedSessionPart(label string, price, previousClose float64) string {
	if price <= 0 {
		return ""
	}
	if previousClose <= 0 {
		return fmt.Sprintf("%s $%.2f", label, price)
	}
	pct := (price - previousClose) / previousClose * 100
	return fmt.Sprintf("%s $%.2f (%+.1f%%)", label, price, pct)
}

// quoteAPIResponse maps v7/finance/quote (no authentication required).
type quoteAPIResponse struct {
	QuoteResponse struct {
		Result []struct {
			Symbol                     string  `json:"symbol"`
			RegularMarketPrice         float64 `json:"regularMarketPrice"`
			RegularMarketPreviousClose float64 `json:"regularMarketPreviousClose"`
			RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
			RegularMarketDayHigh       float64 `json:"regularMarketDayHigh"`
			RegularMarketDayLow        float64 `json:"regularMarketDayLow"`
			RegularMarketVolume        int64   `json:"regularMarketVolume"`
			RegularMarketOpen          float64 `json:"regularMarketOpen"`
			PreMarketPrice             float64 `json:"preMarketPrice"`
			PostMarketPrice            float64 `json:"postMarketPrice"`
			AverageDailyVolume3Month   int64   `json:"averageDailyVolume3Month"`
			AverageDailyVolume10Day    int64   `json:"averageDailyVolume10Day"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteResponse"`
}

// quoteSummaryPriceResponse maps quoteSummary with price and summaryDetail modules.
type quoteSummaryPriceResponse struct {
	QuoteSummary struct {
		Result []struct {
			Price struct {
				Symbol                     string  `json:"symbol"`
				RegularMarketPrice         float64 `json:"regularMarketPrice"`
				RegularMarketPreviousClose float64 `json:"regularMarketPreviousClose"`
				RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
				RegularMarketDayHigh       float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow        float64 `json:"regularMarketDayLow"`
				RegularMarketVolume        int64   `json:"regularMarketVolume"`
				RegularMarketOpen          float64 `json:"regularMarketOpen"`
				PreMarketPrice             float64 `json:"preMarketPrice"`
				PostMarketPrice            float64 `json:"postMarketPrice"`
			} `json:"price"`
			SummaryDetail struct {
				PreviousClose      float64 `json:"previousClose"`
				AverageVolume      int64   `json:"averageVolume"`
				AverageVolume10Day int64   `json:"averageDailyVolume10Day"`
			} `json:"summaryDetail"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// chartExtendedResponse maps chart endpoint with intraday closes and trading periods.
type chartExtendedResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol               string  `json:"symbol"`
				RegularMarketPrice   float64 `json:"regularMarketPrice"`
				PreviousClose        float64 `json:"previousClose"`
				ChartPreviousClose   float64 `json:"chartPreviousClose"`
				RegularMarketDayHigh float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow  float64 `json:"regularMarketDayLow"`
				RegularMarketVolume  int64   `json:"regularMarketVolume"`
				RegularMarketOpen    float64 `json:"regularMarketOpen"`
				CurrentTradingPeriod struct {
					Pre struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"pre"`
					Regular struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"regular"`
					Post struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"post"`
				} `json:"currentTradingPeriod"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// FetchQuoteExtended retrieves extended quote data including pre/post market and average volume.
// Uses the quote endpoint first because it does not require crumb/cookie authentication.
func FetchQuoteExtended(symbol string) (*QuoteExtended, error) {
	endpoint := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s",
		url.QueryEscape(symbol),
	)

	c := getClient()
	body, err := c.doSimpleGet(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch quote endpoint for %s: %w", symbol, err)
	}

	var resp quoteAPIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse quote endpoint response: %w", err)
	}

	if resp.QuoteResponse.Error != nil {
		return nil, fmt.Errorf("quote endpoint error for %s: %s", symbol, resp.QuoteResponse.Error.Description)
	}

	if len(resp.QuoteResponse.Result) == 0 {
		return nil, fmt.Errorf("no quote endpoint data for %s", symbol)
	}

	r := resp.QuoteResponse.Result[0]
	outSymbol := r.Symbol
	if strings.TrimSpace(outSymbol) == "" {
		outSymbol = strings.ToUpper(strings.TrimSpace(symbol))
	}

	avgVol := r.AverageDailyVolume3Month
	if avgVol == 0 {
		avgVol = r.AverageDailyVolume10Day
	}

	return &QuoteExtended{
		Symbol:             outSymbol,
		RegularMarketPrice: r.RegularMarketPrice,
		PreviousClose:      r.RegularMarketPreviousClose,
		ChangePercent:      r.RegularMarketChangePercent,
		DayHigh:            r.RegularMarketDayHigh,
		DayLow:             r.RegularMarketDayLow,
		Volume:             r.RegularMarketVolume,
		AverageVolume:      avgVol,
		PreMarketPrice:     r.PreMarketPrice,
		PostMarketPrice:    r.PostMarketPrice,
		RegularMarketOpen:  r.RegularMarketOpen,
	}, nil
}

// fetchQuoteExtendedFromQuoteSummary is an authenticated fallback for symbols where
// the quote endpoint is sparse.
func fetchQuoteExtendedFromQuoteSummary(symbol string) (*QuoteExtended, error) {
	url := fmt.Sprintf(
		"https://query2.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=price,summaryDetail",
		symbol,
	)

	c := getClient()
	body, err := c.doAuthenticatedGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch quote extended for %s: %w", symbol, err)
	}

	var resp quoteSummaryPriceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse quote extended: %w", err)
	}

	if resp.QuoteSummary.Error != nil {
		return nil, fmt.Errorf("API error for %s: %s", symbol, resp.QuoteSummary.Error.Description)
	}

	if len(resp.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("no quote data for %s", symbol)
	}

	r := resp.QuoteSummary.Result[0]
	p := r.Price
	s := r.SummaryDetail

	prevClose := p.RegularMarketPreviousClose
	if prevClose == 0 {
		prevClose = s.PreviousClose
	}

	avgVol := s.AverageVolume
	if avgVol == 0 {
		avgVol = s.AverageVolume10Day
	}

	outSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.TrimSpace(p.Symbol) != "" {
		outSymbol = p.Symbol
	}

	return &QuoteExtended{
		Symbol:             outSymbol,
		RegularMarketPrice: p.RegularMarketPrice,
		PreviousClose:      prevClose,
		ChangePercent:      p.RegularMarketChangePercent,
		DayHigh:            p.RegularMarketDayHigh,
		DayLow:             p.RegularMarketDayLow,
		Volume:             p.RegularMarketVolume,
		AverageVolume:      avgVol,
		PreMarketPrice:     p.PreMarketPrice,
		PostMarketPrice:    p.PostMarketPrice,
		RegularMarketOpen:  p.RegularMarketOpen,
	}, nil
}

// fetchQuoteExtendedFromChart is a no-auth fallback that also extracts pre/post prices
// from intraday chart data when available.
func fetchQuoteExtendedFromChart(symbol string) (*QuoteExtended, error) {
	endpoint := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=2d&interval=5m&includePrePost=true",
		url.QueryEscape(symbol),
	)

	c := getClient()
	body, err := c.doSimpleGet(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch chart extended for %s: %w", symbol, err)
	}

	var resp chartExtendedResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse chart extended for %s: %w", symbol, err)
	}

	if resp.Chart.Error != nil {
		return nil, fmt.Errorf("chart extended error for %s: %s", symbol, resp.Chart.Error.Description)
	}
	if len(resp.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart extended data for %s", symbol)
	}

	r := resp.Chart.Result[0]
	meta := r.Meta

	prevClose := meta.PreviousClose
	if prevClose == 0 {
		prevClose = meta.ChartPreviousClose
	}

	changePct := 0.0
	if prevClose > 0 {
		changePct = (meta.RegularMarketPrice - prevClose) / prevClose * 100
	}

	pre := 0.0
	post := 0.0
	if len(r.Indicators.Quote) > 0 {
		closes := r.Indicators.Quote[0].Close
		pre = lastCloseInWindow(
			r.Timestamp,
			closes,
			meta.CurrentTradingPeriod.Pre.Start,
			meta.CurrentTradingPeriod.Pre.End,
		)
		post = lastCloseInWindow(
			r.Timestamp,
			closes,
			meta.CurrentTradingPeriod.Post.Start,
			meta.CurrentTradingPeriod.Post.End,
		)
	}

	outSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.TrimSpace(meta.Symbol) != "" {
		outSymbol = meta.Symbol
	}

	return &QuoteExtended{
		Symbol:             outSymbol,
		RegularMarketPrice: meta.RegularMarketPrice,
		PreviousClose:      prevClose,
		ChangePercent:      changePct,
		DayHigh:            meta.RegularMarketDayHigh,
		DayLow:             meta.RegularMarketDayLow,
		Volume:             meta.RegularMarketVolume,
		AverageVolume:      0,
		PreMarketPrice:     pre,
		PostMarketPrice:    post,
		RegularMarketOpen:  meta.RegularMarketOpen,
	}, nil
}

func lastCloseInWindow(timestamps []int64, closes []*float64, start, end int64) float64 {
	if start <= 0 || end <= start {
		return 0
	}

	limit := len(timestamps)
	if len(closes) < limit {
		limit = len(closes)
	}

	last := 0.0
	for i := 0; i < limit; i++ {
		ts := timestamps[i]
		if ts < start || ts >= end {
			continue
		}
		if closes[i] == nil || *closes[i] <= 0 {
			continue
		}
		last = *closes[i]
	}
	return last
}

// FetchQuoteExtendedWithFallback tries quote endpoint first, then quoteSummary,
// then chart data (including pre/post where available).
func FetchQuoteExtendedWithFallback(symbol string) (*QuoteExtended, error) {
	q, err := FetchQuoteExtended(symbol)
	if err == nil {
		return q, nil
	}
	q2, err2 := fetchQuoteExtendedFromQuoteSummary(symbol)
	if err2 == nil {
		return q2, nil
	}
	q3, err3 := fetchQuoteExtendedFromChart(symbol)
	if err3 != nil {
		return nil, fmt.Errorf("extended quote failed for %s (quote endpoint: %v; quoteSummary: %v; chart fallback: %w)", symbol, err, err2, err3)
	}
	return q3, nil
}
