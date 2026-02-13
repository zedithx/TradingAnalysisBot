package yahoo

import (
	"encoding/json"
	"fmt"
)

// chartResponse is the v8 chart API response structure (used for validation).
type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol   string `json:"symbol"`
				Exchange string `json:"exchangeName"`
			} `json:"meta"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// ValidateSymbol checks whether a stock ticker is recognised by Yahoo Finance.
// Uses the v8/finance/chart endpoint which works without crumb authentication.
// Returns nil if valid, or a descriptive error if invalid/unreachable.
func ValidateSymbol(symbol string) error {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		symbol,
	)

	c := getClient()
	body, err := c.doSimpleGet(url)
	if err != nil {
		return fmt.Errorf("symbol %s not found: %w", symbol, err)
	}

	var cr chartResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if cr.Chart.Error != nil {
		return fmt.Errorf("symbol %s not found: %s", symbol, cr.Chart.Error.Description)
	}

	if len(cr.Chart.Result) == 0 {
		return fmt.Errorf("symbol %s not found on Yahoo Finance", symbol)
	}

	return nil
}
