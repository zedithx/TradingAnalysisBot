package scheduler

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"TradingNewsBot/storage"
	"TradingNewsBot/yahoo"
)

// NotifyFunc is a callback that sends an HTML message to a Telegram chat.
type NotifyFunc func(chatID int64, html string)

// Scheduler manages the hourly background news fetcher.
type Scheduler struct {
	cron   *cron.Cron
	store  *storage.Store
	notify NotifyFunc
}

// New creates a new Scheduler. If notify is non-nil, users will receive
// a digest message whenever new articles are found for their watchlist.
func New(store *storage.Store, notify NotifyFunc) *Scheduler {
	return &Scheduler{
		cron:   cron.New(),
		store:  store,
		notify: notify,
	}
}

// Start registers the hourly fetch job and starts the cron scheduler.
func (s *Scheduler) Start() error {
	// Fetch news every hour
	if _, err := s.cron.AddFunc("0 * * * *", s.fetchAllNews); err != nil {
		return err
	}

	// Prune articles older than 7 days, once a day at midnight
	if _, err := s.cron.AddFunc("0 0 * * *", func() {
		if err := s.store.PruneOldArticles(); err != nil {
			log.Printf("Error pruning old articles: %v", err)
		}
	}); err != nil {
		return err
	}

	s.cron.Start()
	log.Println("Scheduler started: fetching news every hour")

	// Run an initial fetch immediately so the cache isn't empty on startup
	go s.fetchAllNews()

	return nil
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("Scheduler stopped")
}

// fetchAllNews iterates all users and fetches news for each of their symbols.
// After fetching, sends a digest of new articles to each user via Telegram.
func (s *Scheduler) fetchAllNews() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Background fetch: PANIC recovered: %v", r)
		}
	}()

	log.Println("Background fetch: starting...")

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		log.Println("Background fetch: no users")
		return
	}

	// Collect all unique symbols across all users
	symbolUsers := make(map[string][]int64) // symbol -> list of chatIDs
	for _, u := range users {
		for _, sym := range u.Symbols {
			symbolUsers[sym] = append(symbolUsers[sym], u.ChatID)
		}
	}

	totalFetched := 0

	// Track new articles per user: chatID -> symbol -> []CachedArticle
	userNewArticles := make(map[int64]map[string][]storage.CachedArticle)

	for symbol, chatIDs := range symbolUsers {
		articles, err := yahoo.FetchRecentNews(symbol, 24*time.Hour)
		if err != nil {
			log.Printf("Background fetch: error for %s: %v", symbol, err)
			continue
		}

		if len(articles) == 0 {
			log.Printf("Background fetch: no recent articles found for %s (all sources returned empty)", symbol)
			continue
		}

		log.Printf("Background fetch: got %d articles for %s", len(articles), symbol)

		// Convert to cached articles
		cached := make([]storage.CachedArticle, 0, len(articles))
		now := time.Now().UTC()
		for _, a := range articles {
			cached = append(cached, storage.CachedArticle{
				Symbol:    symbol,
				Title:     a.Title,
				Link:      a.Link,
				Published: a.Published,
				FetchedAt: now,
			})
		}

		// Store for each user that tracks this symbol
		for _, chatID := range chatIDs {
			added, err := s.store.AddArticles(chatID, symbol, cached)
			if err != nil {
				log.Printf("Background fetch: error storing articles for %s (chat %d): %v", symbol, chatID, err)
				continue
			}
			if len(added) > 0 {
				if userNewArticles[chatID] == nil {
					userNewArticles[chatID] = make(map[string][]storage.CachedArticle)
				}
				userNewArticles[chatID][symbol] = append(userNewArticles[chatID][symbol], added...)
			}
		}

		totalFetched += len(articles)

		// Small delay between symbols to avoid rate-limiting
		time.Sleep(500 * time.Millisecond)
	}

	// Send digest notifications to users with new articles
	if s.notify != nil {
		for chatID, symbolArticles := range userNewArticles {
			msg := buildDigest(symbolArticles)
			if msg != "" {
				s.notify(chatID, msg)
			}
		}
	}

	log.Printf("Background fetch: complete (%d total articles across %d symbols)", totalFetched, len(symbolUsers))
}

// buildDigest constructs an HTML digest message from new articles grouped by symbol.
// Returns empty string if there's nothing to send.
func buildDigest(symbolArticles map[string][]storage.CachedArticle) string {
	if len(symbolArticles) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<b>New headlines for your watchlist</b>\n")

	totalArticles := 0
	for symbol, articles := range symbolArticles {
		if len(articles) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("\n<b>%s</b>\n", symbol))

		// Cap at 5 headlines per symbol to keep the message manageable
		limit := len(articles)
		if limit > 5 {
			limit = 5
		}

		for i := 0; i < limit; i++ {
			a := articles[i]
			sb.WriteString(fmt.Sprintf("• <a href=\"%s\">%s</a>\n", a.Link, escapeHTML(a.Title)))
		}

		if len(articles) > 5 {
			sb.WriteString(fmt.Sprintf("  <i>+%d more</i>\n", len(articles)-5))
		}

		totalArticles += len(articles)
	}

	if totalArticles == 0 {
		return ""
	}

	sb.WriteString("\nUse /news for the full list.")
	return sb.String()
}

// escapeHTML escapes special characters for Telegram HTML parse mode.
func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
