package yahoo

import (
	"encoding/json"
	"fmt"
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

// quoteSummaryPriceResponse maps quoteSummary with price and summaryDetail modules.
type quoteSummaryPriceResponse struct {
	QuoteSummary struct {
		Result []struct {
			Price struct {
				RegularMarketPrice        float64 `json:"regularMarketPrice"`
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

// FetchQuoteExtended retrieves extended quote data including pre/post market and average volume.
func FetchQuoteExtended(symbol string) (*QuoteExtended, error) {
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

	return &QuoteExtended{
		Symbol:             symbol,
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
