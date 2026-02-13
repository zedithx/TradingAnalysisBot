package yahoo

import (
	"encoding/json"
	"fmt"
	"time"
)

// EarningsInfo contains the next earnings date and derived quarter label.
type EarningsInfo struct {
	Symbol       string
	EarningsDate time.Time
	Quarter      string // e.g. "Q1 2026"
}

// calendarEventsResponse maps the relevant part of the quoteSummary response.
type calendarEventsResponse struct {
	QuoteSummary struct {
		Result []struct {
			CalendarEvents struct {
				Earnings struct {
					EarningsDate []struct {
						Raw int64  `json:"raw"` // unix timestamp
						Fmt string `json:"fmt"` // formatted date string
					} `json:"earningsDate"`
				} `json:"earnings"`
			} `json:"calendarEvents"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// FetchEarnings retrieves the next earnings report date for the given symbol.
// Uses authenticated quoteSummary endpoint with cookie/crumb.
func FetchEarnings(symbol string) (*EarningsInfo, error) {
	url := fmt.Sprintf(
		"https://query2.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=calendarEvents",
		symbol,
	)

	c := getClient()
	body, err := c.doAuthenticatedGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch earnings for %s: %w", symbol, err)
	}

	var ce calendarEventsResponse
	if err := json.Unmarshal(body, &ce); err != nil {
		return nil, fmt.Errorf("parse earnings response: %w", err)
	}

	if ce.QuoteSummary.Error != nil {
		return nil, fmt.Errorf("earnings API error for %s: %s", symbol, ce.QuoteSummary.Error.Description)
	}

	if len(ce.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("no earnings data available for %s", symbol)
	}

	earningsDates := ce.QuoteSummary.Result[0].CalendarEvents.Earnings.EarningsDate
	if len(earningsDates) == 0 {
		return nil, fmt.Errorf("no upcoming earnings date for %s", symbol)
	}

	// Use the first (nearest) earnings date
	ts := earningsDates[0].Raw
	earningsTime := time.Unix(ts, 0).UTC()

	return &EarningsInfo{
		Symbol:       symbol,
		EarningsDate: earningsTime,
		Quarter:      deriveQuarter(earningsTime),
	}, nil
}

// deriveQuarter determines the fiscal quarter label from the earnings date.
// Earnings reports are typically released the quarter AFTER the fiscal quarter ends:
//   - Q1 earnings (Jan-Mar) are reported in Apr-May
//   - Q2 earnings (Apr-Jun) are reported in Jul-Aug
//   - Q3 earnings (Jul-Sep) are reported in Oct-Nov
//   - Q4 earnings (Oct-Dec) are reported in Jan-Feb (next year)
func deriveQuarter(d time.Time) string {
	month := d.Month()
	year := d.Year()

	switch {
	case month >= 1 && month <= 2:
		return fmt.Sprintf("Q4 %d", year-1)
	case month >= 3 && month <= 5:
		return fmt.Sprintf("Q1 %d", year)
	case month >= 6 && month <= 8:
		return fmt.Sprintf("Q2 %d", year)
	case month >= 9 && month <= 11:
		return fmt.Sprintf("Q3 %d", year)
	default:
		return fmt.Sprintf("Q3 %d", year)
	}
}
