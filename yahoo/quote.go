package yahoo

import (
	"encoding/json"
	"fmt"
)

// QuoteData holds current price information for a stock.
type QuoteData struct {
	Symbol             string
	Exchange           string
	Currency           string
	RegularMarketPrice float64
	PreviousClose      float64
	Change             float64  // absolute change
	ChangePercent      float64  // percentage change
	DayHigh            float64
	DayLow             float64
	Volume             int64
}

// chartFullResponse extends chartResponse with price metadata.
type chartFullResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				Exchange           string  `json:"exchangeName"`
				Currency           string  `json:"currency"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
				PreviousClose      float64 `json:"previousClose"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`
				RegularMarketDayHigh  float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow   float64 `json:"regularMarketDayLow"`
				RegularMarketVolume   int64   `json:"regularMarketVolume"`
			} `json:"meta"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// FetchQuote retrieves the current price data for a stock symbol
// using the v8/finance/chart endpoint (no auth required).
func FetchQuote(symbol string) (*QuoteData, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		symbol,
	)

	c := getClient()
	body, err := c.doSimpleGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch quote for %s: %w", symbol, err)
	}

	var cr chartFullResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("parse quote response: %w", err)
	}

	if cr.Chart.Error != nil {
		return nil, fmt.Errorf("quote error for %s: %s", symbol, cr.Chart.Error.Description)
	}

	if len(cr.Chart.Result) == 0 {
		return nil, fmt.Errorf("no quote data for %s", symbol)
	}

	meta := cr.Chart.Result[0].Meta
	price := meta.RegularMarketPrice
	prevClose := meta.PreviousClose
	if prevClose == 0 {
		prevClose = meta.ChartPreviousClose
	}

	change := price - prevClose
	changePct := 0.0
	if prevClose != 0 {
		changePct = (change / prevClose) * 100
	}

	return &QuoteData{
		Symbol:             meta.Symbol,
		Exchange:           meta.Exchange,
		Currency:           meta.Currency,
		RegularMarketPrice: price,
		PreviousClose:      prevClose,
		Change:             change,
		ChangePercent:      changePct,
		DayHigh:            meta.RegularMarketDayHigh,
		DayLow:             meta.RegularMarketDayLow,
		Volume:             meta.RegularMarketVolume,
	}, nil
}
