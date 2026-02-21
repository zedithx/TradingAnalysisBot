package yahoo

import (
	"os"
	"testing"
)

func TestFetchQuoteExtendedWithFallback_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Yahoo API test in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("skipping live Yahoo API test (SKIP_NETWORK_TESTS=1)")
	}

	q, err := FetchQuoteExtendedWithFallback("AAPL")
	if err != nil {
		t.Fatalf("FetchQuoteExtendedWithFallback(AAPL): %v", err)
	}
	if q == nil {
		t.Fatal("FetchQuoteExtendedWithFallback returned nil quote")
	}
	if q.Symbol != "AAPL" {
		t.Errorf("Symbol = %q, want AAPL", q.Symbol)
	}
	if q.RegularMarketPrice <= 0 {
		t.Errorf("RegularMarketPrice = %v, want positive", q.RegularMarketPrice)
	}
	if q.PreviousClose <= 0 {
		t.Errorf("PreviousClose = %v, want positive", q.PreviousClose)
	}
	if q.Volume < 0 {
		t.Errorf("Volume = %v, want non-negative", q.Volume)
	}
}

func TestFetchQuote_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Yahoo API test in short mode")
	}
	if os.Getenv("SKIP_NETWORK_TESTS") == "1" {
		t.Skip("skipping live Yahoo API test (SKIP_NETWORK_TESTS=1)")
	}

	d, err := FetchQuote("AAPL")
	if err != nil {
		t.Fatalf("FetchQuote(AAPL): %v", err)
	}
	if d == nil {
		t.Fatal("FetchQuote returned nil")
	}
	if d.Symbol != "AAPL" {
		t.Errorf("Symbol = %q, want AAPL", d.Symbol)
	}
	if d.RegularMarketPrice <= 0 {
		t.Errorf("RegularMarketPrice = %v, want positive", d.RegularMarketPrice)
	}
}

func TestFetchQuoteExtendedWithFallback_InvalidSymbol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	q, err := FetchQuoteExtendedWithFallback("INVALIDSYMBOL12345XYZ")
	if err == nil {
		t.Fatal("expected error for invalid symbol, got nil")
	}
	if q != nil {
		t.Errorf("expected nil quote on error, got %+v", q)
	}
}
