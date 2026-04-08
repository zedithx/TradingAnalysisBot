package scheduler

import (
	"strings"
	"testing"
	"time"

	"TradingNewsBot/analysis"
	"TradingNewsBot/storage"
	"TradingNewsBot/yahoo"
)

type stubGlobalSummariser struct{}

func (stubGlobalSummariser) SummarizeHeadlines(symbol string, headlines []string) (string, error) {
	return "", nil
}

func (stubGlobalSummariser) ClassifyGlobalHeadlineBatch(articles []yahoo.GlobalNewsArticle) (map[string]analysis.GlobalHeadlineAssessment, error) {
	return map[string]analysis.GlobalHeadlineAssessment{}, nil
}

func (stubGlobalSummariser) SummarizeGlobalDigest(articles []storage.GlobalArticle) (string, error) {
	return "Markets are focused on policy and geopolitical risk.", nil
}

func TestBuildGlobalDigest(t *testing.T) {
	s := &Scheduler{summariser: stubGlobalSummariser{}}
	now := time.Date(2026, 4, 9, 10, 30, 0, 0, time.UTC)

	msg := s.buildGlobalDigest(123, []storage.GlobalArticle{
		{
			Title:     "Fed surprises with hawkish signal",
			Link:      "https://example.com/fed",
			Source:    "Reuters",
			Topic:     "Central Banks",
			Summary:   "Higher-for-longer expectations lifted yields.",
			Published: now,
		},
		{
			Title:     "OPEC weighs deeper oil cuts",
			Link:      "https://example.com/oil",
			Source:    "Bloomberg",
			Topic:     "Energy",
			Published: now.Add(-time.Hour),
		},
	})

	for _, want := range []string{
		"Global market news",
		"Markets are focused on policy and geopolitical risk.",
		"https://example.com/fed",
		"Central Banks",
		"Reuters",
		"Higher-for-longer expectations lifted yields.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("digest missing %q:\n%s", want, msg)
		}
	}
}
