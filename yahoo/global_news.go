package yahoo

import (
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"
)

// GlobalNewsQuery defines one global-news search lane.
type GlobalNewsQuery struct {
	Topic string
	Query string
}

// GlobalNewsArticle is a broad market-moving article candidate.
type GlobalNewsArticle struct {
	Title     string
	Link      string
	Published time.Time
	Source    string
	Topic     string
}

// GlobalNewsQueries is the curated query set for macro/global market movers.
var GlobalNewsQueries = []GlobalNewsQuery{
	{Topic: "Geopolitics", Query: "war OR ceasefire OR missile OR invasion markets"},
	{Topic: "Central Banks", Query: "central bank OR fed OR ecb OR boj rates inflation"},
	{Topic: "M&A", Query: "merger OR acquisition OR takeover OR stake sale"},
	{Topic: "Policy", Query: "sanctions OR export controls OR tariffs"},
	{Topic: "Energy", Query: "oil OPEC supply disruption"},
	{Topic: "Financial Stress", Query: "bank failure OR liquidity crisis OR default"},
}

// FetchGlobalNews retrieves recent broad-market headlines from curated RSS queries.
func FetchGlobalNews(maxAge time.Duration) ([]GlobalNewsArticle, error) {
	c := getClient()
	seen := make(map[string]bool)
	var result []GlobalNewsArticle
	var failures int

	for _, q := range GlobalNewsQueries {
		feedURL := fmt.Sprintf(
			"https://news.google.com/rss/search?q=%s&hl=en&gl=US&ceid=US:en",
			url.QueryEscape(q.Query),
		)

		body, err := c.doSimpleGet(feedURL)
		if err != nil {
			log.Printf("Global News RSS failed for topic %q: %v", q.Topic, err)
			failures++
			continue
		}

		articles, err := parseRSSFeed(body, maxAge)
		if err != nil {
			log.Printf("Global News RSS parse failed for topic %q: %v", q.Topic, err)
			failures++
			continue
		}

		for _, a := range articles {
			if a.Link == "" || seen[a.Link] {
				continue
			}
			seen[a.Link] = true
			result = append(result, GlobalNewsArticle{
				Title:     cleanGlobalTitle(a.Title),
				Link:      a.Link,
				Published: a.Published,
				Source:    a.Source,
				Topic:     q.Topic,
			})
		}

		time.Sleep(200 * time.Millisecond)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Published.After(result[j].Published)
	})

	if len(result) == 0 && failures == len(GlobalNewsQueries) {
		return nil, fmt.Errorf("global news fetch failed for all queries")
	}

	return result, nil
}

// FilterGlobalNewsCandidates applies a fast keyword gate before AI classification.
func FilterGlobalNewsCandidates(articles []GlobalNewsArticle) []GlobalNewsArticle {
	var filtered []GlobalNewsArticle
	for _, a := range articles {
		if isLikelyGlobalMarketMover(a.Title, a.Topic) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func isLikelyGlobalMarketMover(title, topic string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + topic))
	if text == "" {
		return false
	}

	positiveTerms := []string{
		"war", "ceasefire", "missile", "attack", "invasion", "troops", "strike",
		"fed", "federal reserve", "ecb", "boj", "rate", "inflation", "cpi", "jobs report",
		"merger", "acquisition", "takeover", "buyout", "stake", "deal",
		"sanction", "tariff", "export control", "regulation", "probe", "ban",
		"opec", "oil", "crude", "supply disruption", "pipeline",
		"bank failure", "default", "liquidity", "debt crisis", "restructuring", "bailout",
		"nvidia", "apple", "microsoft", "tesla", "amazon", "google", "meta", "tsmc",
	}
	negativeTerms := []string{
		"sports", "celebrity", "movie", "tv", "fashion", "travel", "recipe",
		"earnings preview", "stock picks", "analyst says", "opinion", "podcast", "newsletter",
	}

	for _, term := range negativeTerms {
		if strings.Contains(text, term) {
			return false
		}
	}
	for _, term := range positiveTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func cleanGlobalTitle(title string) string {
	title = cleanTitle(title)
	title = strings.TrimSpace(title)
	return strings.Join(strings.Fields(title), " ")
}
