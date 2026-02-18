package yahoo

import (
	"encoding/json"
	"fmt"
)

// Candle is a single OHLC bar.
type Candle struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
}

// ChartData holds OHLC data from the chart API.
type ChartData struct {
	Symbol       string
	PreviousClose float64
	Candles      []Candle
}

// chartOHLCResponse maps the v8 chart API response.
type chartOHLCResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				PreviousClose      float64 `json:"previousClose"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`
			} `json:"meta"`
			Timestamp  []int64    `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open  []float64 `json:"open"`
					High  []float64 `json:"high"`
					Low   []float64 `json:"low"`
					Close []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// FetchChart retrieves OHLC data for a symbol. rangeStr: "1d", "5d", "1mo", etc. interval: "1m", "1d", etc.
func FetchChart(symbol, rangeStr, interval string) (*ChartData, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s",
		symbol, rangeStr, interval,
	)

	c := getClient()
	body, err := c.doSimpleGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch chart for %s: %w", symbol, err)
	}

	var resp chartOHLCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse chart: %w", err)
	}

	if resp.Chart.Error != nil {
		return nil, fmt.Errorf("chart error for %s: %s", symbol, resp.Chart.Error.Description)
	}

	if len(resp.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart data for %s", symbol)
	}

	res := resp.Chart.Result[0]
	meta := res.Meta
	prevClose := meta.PreviousClose
	if prevClose == 0 {
		prevClose = meta.ChartPreviousClose
	}

	var candles []Candle
	if len(res.Indicators.Quote) > 0 {
		q := res.Indicators.Quote[0]
		for i, ts := range res.Timestamp {
			c := Candle{Timestamp: ts}
			if i < len(q.Open) {
				c.Open = q.Open[i]
			}
			if i < len(q.High) {
				c.High = q.High[i]
			}
			if i < len(q.Low) {
				c.Low = q.Low[i]
			}
			if i < len(q.Close) {
				c.Close = q.Close[i]
			}
			candles = append(candles, c)
		}
	}

	return &ChartData{
		Symbol:        meta.Symbol,
		PreviousClose: prevClose,
		Candles:       candles,
	}, nil
}

// ThirtyDayHigh returns the 30-day high from chart data (use range=1mo, interval=1d).
func (c *ChartData) ThirtyDayHigh() float64 {
	var max float64
	for _, bar := range c.Candles {
		if bar.High > max {
			max = bar.High
		}
	}
	return max
}

// FirstOpen returns the open of the first candle (e.g. market open for range=1d interval=1m).
func (c *ChartData) FirstOpen() float64 {
	if len(c.Candles) == 0 {
		return 0
	}
	return c.Candles[0].Open
}
