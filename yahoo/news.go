package yahoo

import (
	"encoding/xml"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

// rssRoot is the top-level XML structure of an RSS feed.
type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	Source  string `xml:"source"`
}

// NewsArticle represents a single news article for a stock.
type NewsArticle struct {
	Title     string
	Link      string
	Published time.Time
	Source    string
}

// rssFeedURLs lists Yahoo Finance RSS endpoints to try in order.
// Note: feeds.finance.yahoo.com is deprecated and returns stale/frozen data.
// finance.yahoo.com/rss/headline is the confirmed working endpoint.
var rssFeedURLs = []string{
	"https://finance.yahoo.com/rss/headline?s=%s",
}

// FetchNews retrieves news articles for the given stock symbol.
// Tries Yahoo Finance RSS first; if no articles found, falls back to Google News.
// Returns all articles (no time filter).
func FetchNews(symbol string) ([]NewsArticle, error) {
	articles, err := fetchFromYahoo(symbol, 0)
	if err == nil && len(articles) > 0 {
		return articles, nil
	}

	// Fallback to Google News
	log.Printf("Yahoo RSS had no results for %s, trying Google News", symbol)
	gArticles, gErr := fetchFromGoogleNews(symbol)
	if gErr == nil && len(gArticles) > 0 {
		return gArticles, nil
	}

	// Return the original Yahoo error if Google also failed
	if err != nil {
		return nil, err
	}
	if gErr != nil {
		return nil, gErr
	}
	return nil, fmt.Errorf("no news found for %s from any source", symbol)
}

// FetchRecentNews is like FetchNews but only returns articles from the last maxAge duration.
// Used by the background fetcher to only cache fresh articles.
func FetchRecentNews(symbol string, maxAge time.Duration) ([]NewsArticle, error) {
	articles, err := fetchFromYahoo(symbol, maxAge)
	if err == nil && len(articles) > 0 {
		return articles, nil
	}

	// Fallback to Google News with time filter
	log.Printf("Yahoo RSS had no recent results for %s, trying Google News", symbol)
	gArticles, gErr := fetchFromGoogleNews(symbol)
	if gErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, gErr
	}

	// Apply time filter to Google News results
	if maxAge > 0 {
		cutoff := time.Now().UTC().Add(-maxAge)
		var filtered []NewsArticle
		for _, a := range gArticles {
			if a.Published.After(cutoff) {
				filtered = append(filtered, a)
			}
		}
		return filtered, nil
	}

	return gArticles, nil
}

// fetchFromYahoo fetches news from Yahoo Finance RSS feeds.
func fetchFromYahoo(symbol string, maxAge time.Duration) ([]NewsArticle, error) {
	c := getClient()
	var lastErr error

	for _, urlTemplate := range rssFeedURLs {
		feedURL := fmt.Sprintf(urlTemplate, symbol)

		body, err := c.doSimpleGet(feedURL)
		if err != nil {
			log.Printf("Yahoo RSS failed for %s (%s): %v", symbol, feedURL, err)
			lastErr = err
			continue
		}

		articles, err := parseRSSFeed(body, maxAge)
		if err != nil {
			log.Printf("Yahoo RSS parse failed for %s (%s): %v", symbol, feedURL, err)
			lastErr = err
			continue
		}

		if len(articles) == 0 {
			log.Printf("Yahoo RSS empty for %s (%s), trying next", symbol, feedURL)
			lastErr = fmt.Errorf("no articles from Yahoo for %s", symbol)
			continue
		}

		log.Printf("Yahoo RSS: got %d articles for %s", len(articles), symbol)
		return articles, nil
	}

	return nil, lastErr
}

// fetchFromGoogleNews fetches news from Google News RSS search.
func fetchFromGoogleNews(symbol string) ([]NewsArticle, error) {
	query := url.QueryEscape(symbol + " stock")
	feedURL := fmt.Sprintf("https://news.google.com/rss/search?q=%s&hl=en&gl=US&ceid=US:en", query)

	c := getClient()
	body, err := c.doSimpleGet(feedURL)
	if err != nil {
		return nil, fmt.Errorf("Google News RSS failed for %s: %w", symbol, err)
	}

	articles, err := parseRSSFeed(body, 0)
	if err != nil {
		return nil, fmt.Errorf("Google News RSS parse failed for %s: %w", symbol, err)
	}

	log.Printf("Google News: got %d articles for %s", len(articles), symbol)
	return articles, nil
}

// parseRSSFeed parses an RSS XML body into NewsArticles.
// If maxAge > 0, articles older than that are filtered out.
func parseRSSFeed(body []byte, maxAge time.Duration) ([]NewsArticle, error) {
	var rss rssRoot
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("parse RSS XML: %w", err)
	}

	var cutoff time.Time
	if maxAge > 0 {
		cutoff = time.Now().UTC().Add(-maxAge)
	}

	var articles []NewsArticle
	for _, item := range rss.Channel.Items {
		pub, err := parseRSSDate(item.PubDate)
		if err != nil {
			pub = time.Now().UTC()
		}

		if maxAge > 0 && !pub.After(cutoff) {
			continue
		}

		title := cleanTitle(item.Title)
		link := item.Link
		source := item.Source

		articles = append(articles, NewsArticle{
			Title:     title,
			Link:      link,
			Published: pub,
			Source:    source,
		})
	}

	return articles, nil
}

// cleanTitle removes the trailing " - Source Name" that Google News appends to titles.
func cleanTitle(title string) string {
	if idx := strings.LastIndex(title, " - "); idx > 0 {
		// Only strip if the part after " - " looks like a source name (short)
		suffix := title[idx+3:]
		if len(suffix) < 60 {
			return title[:idx]
		}
	}
	return title
}

// parseRSSDate tries several common RSS date formats.
func parseRSSDate(s string) (time.Time, error) {
	formats := []string{
		time.RFC1123Z,                    // Mon, 02 Jan 2006 15:04:05 -0700
		time.RFC1123,                     // Mon, 02 Jan 2006 15:04:05 MST
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 +0000",
		"2006-01-02T15:04:05Z",
		time.RFC3339,                     // 2006-01-02T15:04:05Z07:00
		"Mon, 02 Jan 2006 15:04:05 GMT",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", s)
}
