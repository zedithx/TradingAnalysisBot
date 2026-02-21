package yahoo

import "math"

// ComputeMA returns the simple moving average of the last `period` closes.
func ComputeMA(candles []Candle, period int) float64 {
	if len(candles) == 0 || period <= 0 {
		return 0
	}
	n := len(candles)
	if period > n {
		period = n
	}
	sum := 0.0
	for i := n - period; i < n; i++ {
		sum += candles[i].Close
	}
	return sum / float64(period)
}

// ComputeRSI returns the RSI for the given period (typically 14).
func ComputeRSI(candles []Candle, period int) float64 {
	if len(candles) < period+1 {
		return 0
	}
	n := len(candles)
	gains := 0.0
	losses := 0.0
	for i := n - period; i < n; i++ {
		chg := candles[i].Close - candles[i-1].Close
		if chg > 0 {
			gains += chg
		} else {
			losses += -chg
		}
	}
	if losses == 0 {
		return 100
	}
	rs := gains / losses
	return 100 - (100 / (1 + rs))
}

// RecentHighsLows returns the high and low over the last n candles.
func RecentHighsLows(candles []Candle, n int) (high, low float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	start := len(candles) - n
	if start < 0 {
		start = 0
	}
	high = candles[start].High
	low = candles[start].Low
	for i := start; i < len(candles); i++ {
		if candles[i].High > high {
			high = candles[i].High
		}
		if candles[i].Low < low {
			low = candles[i].Low
		}
	}
	return high, low
}

// TechnicalSnapshot holds computed technicals for a symbol.
type TechnicalSnapshot struct {
	MA50       float64
	MA200      float64
	RSI        float64
	RecentHigh float64
	RecentLow  float64
	PriceVs50  string  // "above", "below"
	PriceVs200 string
	Dist50Pct  float64 // distance from 50 MA as %
}

// ComputeTechnicals fetches chart data and computes MA, RSI, S/R.
func ComputeTechnicals(symbol string) (*TechnicalSnapshot, error) {
	chart, err := FetchChart(symbol, "1y", "1d")
	if err != nil {
		return nil, err
	}
	if len(chart.Candles) == 0 {
		return nil, nil
	}

	price := chart.Candles[len(chart.Candles)-1].Close
	ma50 := ComputeMA(chart.Candles, 50)
	ma200 := ComputeMA(chart.Candles, 200)
	rsi := ComputeRSI(chart.Candles, 14)
	high20, low20 := RecentHighsLows(chart.Candles, 20)

	snap := &TechnicalSnapshot{
		MA50:       ma50,
		MA200:      ma200,
		RSI:        math.Round(rsi*10) / 10,
		RecentHigh: high20,
		RecentLow:  low20,
	}
	if ma50 > 0 {
		dist := (price - ma50) / ma50 * 100
		snap.Dist50Pct = math.Round(dist*10) / 10
		if price >= ma50 {
			snap.PriceVs50 = "above"
		} else {
			snap.PriceVs50 = "below"
		}
	}
	if ma200 > 0 {
		if price >= ma200 {
			snap.PriceVs200 = "above"
		} else {
			snap.PriceVs200 = "below"
		}
	}
	return snap, nil
}
