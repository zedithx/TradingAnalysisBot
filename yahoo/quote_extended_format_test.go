package yahoo

import "testing"

func TestQuoteExtended_SessionPriceSummary(t *testing.T) {
	q := &QuoteExtended{
		RegularMarketPrice: 103.12,
		ChangePercent:      1.2,
		PreviousClose:      101.90,
		PreMarketPrice:     104.25,
		PostMarketPrice:    102.30,
	}

	got := q.SessionPriceSummary()
	want := "Regular $103.12 (+1.2%) | Pre-market $104.25 (+2.3%) | Post-market $102.30 (+0.4%)"
	if got != want {
		t.Fatalf("SessionPriceSummary() = %q, want %q", got, want)
	}
}

func TestQuoteExtended_SessionPriceSummary_NoPreviousClose(t *testing.T) {
	q := &QuoteExtended{
		RegularMarketPrice: 0,
		PreviousClose:      0,
		PreMarketPrice:     88.5,
	}

	got := q.SessionPriceSummary()
	want := "Pre-market $88.50"
	if got != want {
		t.Fatalf("SessionPriceSummary() = %q, want %q", got, want)
	}
}
