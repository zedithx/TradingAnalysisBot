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

// NewsSummariser can summarize headlines for a symbol.
type NewsSummariser interface {
	SummarizeHeadlines(symbol string, headlines []string) (string, error)
}

// NotifyFunc is a callback that sends an HTML message to a Telegram chat.
type NotifyFunc func(chatID int64, html string)

// Scheduler manages the background news fetcher (every 4 hours) and earnings reminders.
type Scheduler struct {
	cron      *cron.Cron
	store     *storage.Store
	notify    NotifyFunc
	summariser NewsSummariser
}

// New creates a new Scheduler. If notify is non-nil, users receive digest messages.
// If summariser is non-nil, digests include AI summaries per company instead of raw headlines.
func New(store *storage.Store, notify NotifyFunc, summariser NewsSummariser) *Scheduler {
	return &Scheduler{
		cron:       cron.New(),
		store:      store,
		notify:     notify,
		summariser: summariser,
	}
}

// Start registers the fetch job (every 4 hours) and starts the cron scheduler.
func (s *Scheduler) Start() error {
	// Fetch news every 4 hours (0:00, 4:00, 8:00, 12:00, 16:00, 20:00 UTC)
	if _, err := s.cron.AddFunc("0 */4 * * *", s.fetchAllNews); err != nil {
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

	// Earnings reminder: 1 day before report, daily at 9 AM UTC
	if _, err := s.cron.AddFunc("0 9 * * *", s.sendEarningsReminders); err != nil {
		return err
	}

	// Trial expiry reminders: 5 days left, 1 day left, expired — daily at 10 AM UTC
	if _, err := s.cron.AddFunc("0 10 * * *", s.sendTrialReminders); err != nil {
		return err
	}

	// Intraday alerts: every 15 min during US market hours (14:00-20:59 UTC) Mon-Fri
	if _, err := s.cron.AddFunc("*/15 14-20 * * 1-5", s.checkIntradayAlerts); err != nil {
		return err
	}

	// Morning premarket snapshot: weekdays at 12:00 UTC (~7 AM ET)
	if _, err := s.cron.AddFunc("0 12 * * 1-5", s.sendMorningSnapshot); err != nil {
		return err
	}

	// After-hours mover alerts: 22:00 and 02:00 UTC (catches US post-market)
	if _, err := s.cron.AddFunc("0 22 * * 1-5", s.checkAfterHoursAlerts); err != nil {
		return err
	}
	if _, err := s.cron.AddFunc("0 2 * * 2-6", s.checkAfterHoursAlerts); err != nil {
		return err
	}

	// Weekly recap: Sunday 18:00 UTC
	if _, err := s.cron.AddFunc("0 18 * * 0", s.sendWeeklyRecap); err != nil {
		return err
	}

	s.cron.Start()
	log.Println("Scheduler started: fetching news every 4 hours (with AI summary), earnings reminders at 9 AM UTC")

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
			msg := s.buildDigest(symbolArticles)
			if msg != "" {
				s.notify(chatID, msg)
			}
		}
	}

	log.Printf("Background fetch: complete (%d total articles across %d symbols)", totalFetched, len(symbolUsers))
}

// sendEarningsReminders checks each user's watchlist for earnings reports tomorrow and sends reminders.
func (s *Scheduler) sendEarningsReminders() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Earnings reminder: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	log.Println("Earnings reminder: starting...")

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		return
	}

	tomorrow := time.Now().UTC().Add(24 * time.Hour)
	tomorrowDate := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	// chatID -> list of (symbol, quarter) with earnings tomorrow
	userReminders := make(map[int64][]struct {
		symbol string
		quarter string
		date time.Time
	})

	for _, u := range users {
		for _, symbol := range u.Symbols {
			info, err := yahoo.FetchEarnings(symbol)
			if err != nil {
				log.Printf("Earnings reminder: %s: %v", symbol, err)
				time.Sleep(300 * time.Millisecond)
				continue
			}

			earningsDate := time.Date(info.EarningsDate.Year(), info.EarningsDate.Month(), info.EarningsDate.Day(), 0, 0, 0, 0, time.UTC)
			if earningsDate.Equal(tomorrowDate) {
				userReminders[u.ChatID] = append(userReminders[u.ChatID], struct {
					symbol  string
					quarter string
					date   time.Time
				}{symbol, info.Quarter, info.EarningsDate})
			}

			time.Sleep(300 * time.Millisecond)
		}
	}

	for chatID, list := range userReminders {
		if len(list) == 0 {
			continue
		}
		var sb strings.Builder
		sb.WriteString("<b>Earnings tomorrow</b>\n\n")
		for _, r := range list {
			sb.WriteString(fmt.Sprintf("• <b>%s</b> — %s (%s)\n", r.symbol, r.date.Format("Jan 02, 2006"), r.quarter))
		}
		sb.WriteString("\nUse /reports to see all upcoming earnings.")
		s.notify(chatID, sb.String())
	}

	log.Printf("Earnings reminder: sent to %d users", len(userReminders))
}

// sendTrialReminders notifies users: 5 days before free trial ends, 1 day before, and when expired.
func (s *Scheduler) sendTrialReminders() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Trial reminder: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	candidates, err := s.store.GetTrialReminderCandidates()
	if err != nil {
		log.Printf("Trial reminder: GetTrialReminderCandidates error: %v", err)
		return
	}

	now := time.Now().UTC()
	sent5d, sent1d, sentExpired := 0, 0, 0

	for _, c := range candidates {
		trialEnd := c.FirstUsedAt.Add(30 * 24 * time.Hour)
		hoursUntil := trialEnd.Sub(now).Hours()
		daysUntil := hoursUntil / 24

		if !c.Reminder5dSent && daysUntil >= 4 && daysUntil < 6 {
			msg := `<b>5 days left on your free trial</b>

Your 30-day free trial of TradingNewsBot ends in 5 days. Subscribe before then to keep access to your watchlist, news, earnings reminders, and AI analysis.

Use /start to subscribe (100 Stars/month).`
			s.notify(c.ChatID, msg)
			if err := s.store.MarkTrialReminder5dSent(c.ChatID); err != nil {
				log.Printf("Trial reminder: MarkTrialReminder5dSent %d: %v", c.ChatID, err)
			} else {
				sent5d++
			}
		} else if !c.Reminder1dSent && daysUntil >= 0.5 && daysUntil < 1.5 {
			msg := `<b>1 day left on your free trial</b>

Your free trial ends tomorrow. Subscribe now to keep uninterrupted access.

Use /start to subscribe.`
			s.notify(c.ChatID, msg)
			if err := s.store.MarkTrialReminder1dSent(c.ChatID); err != nil {
				log.Printf("Trial reminder: MarkTrialReminder1dSent %d: %v", c.ChatID, err)
			} else {
				sent1d++
			}
		} else if !c.ReminderExpiredSent && daysUntil < 0 {
			msg := `<b>Your free trial has expired</b>

Your 30-day free trial of TradingNewsBot has ended. Subscribe to regain access to your watchlist, news, earnings, and AI analysis.

Use /start to subscribe (100 Stars/month). /support for help.`
			s.notify(c.ChatID, msg)
			if err := s.store.MarkTrialReminderExpiredSent(c.ChatID); err != nil {
				log.Printf("Trial reminder: MarkTrialReminderExpiredSent %d: %v", c.ChatID, err)
			} else {
				sentExpired++
			}
		}
	}

	if sent5d > 0 || sent1d > 0 || sentExpired > 0 {
		log.Printf("Trial reminder: sent 5d=%d, 1d=%d, expired=%d", sent5d, sent1d, sentExpired)
	}
}

// checkIntradayAlerts checks for intraday 3%, volume 2x, 30d breakout, gap 5% — during US market hours.
func (s *Scheduler) checkIntradayAlerts() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Intraday alerts: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		return
	}

	// symbol -> list of chatIDs tracking it
	symbolToChatIDs := make(map[string][]int64)
	for _, u := range users {
		for _, sym := range u.Symbols {
			symbolToChatIDs[sym] = append(symbolToChatIDs[sym], u.ChatID)
		}
	}

	// chatID -> list of alert lines
	userAlerts := make(map[int64][]string)

	for symbol, chatIDs := range symbolToChatIDs {
		q, err := yahoo.FetchQuoteExtendedWithFallback(symbol)
		if err != nil {
			log.Printf("Intraday alerts %s: %v", symbol, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		time.Sleep(200 * time.Millisecond)

		chart1mo, _ := yahoo.FetchChart(symbol, "1mo", "1d")
		time.Sleep(200 * time.Millisecond)
		chart1d, _ := yahoo.FetchChart(symbol, "1d", "1m")

		var triggered []string
		// Marginal move alert (1-3%): "stock increased marginally"
		if (q.ChangePercent >= 1 && q.ChangePercent < 3) || (q.ChangePercent <= -1 && q.ChangePercent > -3) {
			triggered = append(triggered, fmt.Sprintf("%+.1f%% (marginal)", q.ChangePercent))
		}
		// Significant move (3%+)
		if q.ChangePercent >= 3 || q.ChangePercent <= -3 {
			triggered = append(triggered, fmt.Sprintf("%+.1f%%", q.ChangePercent))
		}
		if q.AverageVolume > 0 && q.Volume >= 2*q.AverageVolume {
			triggered = append(triggered, fmt.Sprintf("Volume %.1fx avg", float64(q.Volume)/float64(q.AverageVolume)))
		}
		thirtyHigh := 0.0
		if chart1mo != nil {
			thirtyHigh = chart1mo.ThirtyDayHigh()
		}
		if thirtyHigh > 0 && q.DayHigh >= thirtyHigh {
			triggered = append(triggered, "Broke 30d high")
		}
		gapPct := 0.0
		if q.PreviousClose > 0 && chart1d != nil && len(chart1d.Candles) > 0 {
			firstOpen := chart1d.FirstOpen()
			if firstOpen > 0 {
				gapPct = (firstOpen - q.PreviousClose) / q.PreviousClose * 100
			}
		}
		if gapPct >= 5 {
			triggered = append(triggered, fmt.Sprintf("Gap up %.1f%%", gapPct))
		}

		if len(triggered) == 0 {
			continue
		}

		line := fmt.Sprintf("<b>%s</b>: %s", symbol, strings.Join(triggered, " | "))
		// Use separate trigger types: intraday_marginal (1-3%) vs intraday (3%+)
		triggerType := "intraday"
		for _, t := range triggered {
			if strings.Contains(t, "marginal") {
				triggerType = "intraday_marginal"
				break
			}
		}

		for _, chatID := range chatIDs {
			eligible, _ := s.store.IsEligible(chatID)
			if !eligible {
				continue
			}
			ok, _ := s.store.WasAlertTriggeredInLast24h(chatID, symbol, triggerType)
			if ok {
				continue
			}
			userAlerts[chatID] = append(userAlerts[chatID], line)
			_ = s.store.RecordAlertTrigger(chatID, symbol, triggerType)
		}
	}

	for chatID, lines := range userAlerts {
		if len(lines) == 0 {
			continue
		}
		msg := "<b>Intraday alerts</b>\n\n" + strings.Join(lines, "\n")
		s.notify(chatID, msg)
	}

	if len(userAlerts) > 0 {
		log.Printf("Intraday alerts: sent to %d users", len(userAlerts))
	}
}

// sendMorningSnapshot sends a weekday premarket snapshot: top gainer, loser, biggest move, earnings today.
func (s *Scheduler) sendMorningSnapshot() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Morning snapshot: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		return
	}

	today := time.Now().UTC()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	for _, u := range users {
		if len(u.Symbols) == 0 {
			continue
		}
		eligible, _ := s.store.IsEligible(u.ChatID)
		if !eligible {
			continue
		}

		type premarketMove struct {
			symbol string
			pct   float64
		}
		var moves []premarketMove
		var regularSnapshot []struct {
			symbol string
			price  float64
			pct    float64
		}
		var earningsToday []string

		for _, symbol := range u.Symbols {
			q, err := yahoo.FetchQuoteExtendedWithFallback(symbol)
			if err != nil {
				log.Printf("Morning snapshot %s: %v", symbol, err)
				time.Sleep(300 * time.Millisecond)
				continue
			}
			time.Sleep(200 * time.Millisecond)

			if q.PreMarketPrice > 0 && q.PreviousClose > 0 {
				pct := (q.PreMarketPrice - q.PreviousClose) / q.PreviousClose * 100
				moves = append(moves, premarketMove{symbol, pct})
			} else {
				// No premarket data — use regular session for overview
				regularSnapshot = append(regularSnapshot, struct {
					symbol string
					price  float64
					pct    float64
				}{symbol, q.RegularMarketPrice, q.ChangePercent})
			}

			info, err := yahoo.FetchEarnings(symbol)
			if err == nil {
				ed := time.Date(info.EarningsDate.Year(), info.EarningsDate.Month(), info.EarningsDate.Day(), 0, 0, 0, 0, time.UTC)
				if ed.Equal(todayDate) {
					earningsToday = append(earningsToday, symbol)
				}
			}
			time.Sleep(200 * time.Millisecond)
		}

		// Always send something if user has symbols
		if len(moves) == 0 && len(regularSnapshot) == 0 && len(earningsToday) == 0 {
			continue
		}

		var sb strings.Builder
		sb.WriteString("<b>Good morning. Here's your watchlist:</b>\n\n")

		if len(moves) > 0 {
			topGain := moves[0]
			topLose := moves[0]
			biggestAbs := moves[0]
			for _, m := range moves[1:] {
				if m.pct > topGain.pct {
					topGain = m
				}
				if m.pct < topLose.pct {
					topLose = m
				}
				absM := m.pct
				if absM < 0 {
					absM = -absM
				}
				absBig := biggestAbs.pct
				if absBig < 0 {
					absBig = -absBig
				}
				if absM > absBig {
					biggestAbs = m
				}
			}
			sb.WriteString(fmt.Sprintf("• Top gainer: <b>%s</b> %+.1f%%\n", topGain.symbol, topGain.pct))
			sb.WriteString(fmt.Sprintf("• Top loser: <b>%s</b> %+.1f%%\n", topLose.symbol, topLose.pct))
			sb.WriteString(fmt.Sprintf("• Biggest move: <b>%s</b> %+.1f%%\n", biggestAbs.symbol, biggestAbs.pct))
		}
		if len(earningsToday) > 0 {
			sb.WriteString(fmt.Sprintf("\n• Earnings today: %s\n", strings.Join(earningsToday, ", ")))
		}
		// Include regular market snapshot when no premarket or as supplement
		if len(regularSnapshot) > 0 {
			sb.WriteString("\n")
			for _, r := range regularSnapshot {
				sb.WriteString(fmt.Sprintf("• <b>%s</b> $%.2f %+.1f%%\n", r.symbol, r.price, r.pct))
			}
		}

		s.notify(u.ChatID, sb.String())
		time.Sleep(100 * time.Millisecond)
	}

		log.Println("Morning snapshot: sent")
}

// checkAfterHoursAlerts notifies when a watchlist stock moves >5% in after-hours.
func (s *Scheduler) checkAfterHoursAlerts() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("After-hours alerts: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		return
	}

	symbolToChatIDs := make(map[string][]int64)
	for _, u := range users {
		eligible, _ := s.store.IsEligible(u.ChatID)
		if !eligible {
			continue
		}
		for _, sym := range u.Symbols {
			symbolToChatIDs[sym] = append(symbolToChatIDs[sym], u.ChatID)
		}
	}

	userAlerts := make(map[int64][]string)
	for symbol, chatIDs := range symbolToChatIDs {
		q, err := yahoo.FetchQuoteExtendedWithFallback(symbol)
		if err != nil {
			log.Printf("After-hours %s: %v", symbol, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		time.Sleep(200 * time.Millisecond)

		if q.PostMarketPrice <= 0 || q.PreviousClose <= 0 {
			continue
		}
		movePct := (q.PostMarketPrice - q.PreviousClose) / q.PreviousClose * 100
		if movePct >= -5 && movePct <= 5 {
			continue
		}

		for _, chatID := range chatIDs {
			ok, _ := s.store.WasAlertTriggeredInLast24h(chatID, symbol, "afterhours_5pct")
			if ok {
				continue
			}
			userAlerts[chatID] = append(userAlerts[chatID], fmt.Sprintf("<b>%s</b> %+.1f%%", symbol, movePct))
			_ = s.store.RecordAlertTrigger(chatID, symbol, "afterhours_5pct")
		}
	}

	for chatID, lines := range userAlerts {
		if len(lines) == 0 {
			continue
		}
		msg := "<b>After-hours mover alert</b>\n\n" + strings.Join(lines, "\n") + "\n\n(post-market)"
		s.notify(chatID, msg)
	}

	if len(userAlerts) > 0 {
		log.Printf("After-hours alerts: sent to %d users", len(userAlerts))
	}
}

// sendWeeklyRecap sends a Sunday recap: best/worst performer, avg move, biggest news, upcoming catalysts.
func (s *Scheduler) sendWeeklyRecap() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Weekly recap: PANIC recovered: %v", r)
		}
	}()

	if s.notify == nil {
		return
	}

	users := s.store.GetAllUsers()
	if len(users) == 0 {
		return
	}

	now := time.Now().UTC()
	nextWeek := now.Add(7 * 24 * time.Hour)

	for _, u := range users {
		if len(u.Symbols) == 0 {
			continue
		}
		eligible, _ := s.store.IsEligible(u.ChatID)
		if !eligible {
			continue
		}

		type perf struct {
			symbol string
			pct   float64
		}
		var perfs []perf

		for _, symbol := range u.Symbols {
			chart, err := yahoo.FetchChart(symbol, "5d", "1d")
			if err != nil || chart == nil || len(chart.Candles) < 2 {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			time.Sleep(200 * time.Millisecond)

			firstOpen := chart.Candles[0].Open
			lastClose := chart.Candles[len(chart.Candles)-1].Close
			if firstOpen <= 0 {
				continue
			}
			pct := (lastClose - firstOpen) / firstOpen * 100
			perfs = append(perfs, perf{symbol, pct})
		}

		articles := s.store.GetLatestArticles(u.ChatID, "", 5)
		biggestNews := ""
		if len(articles) > 0 {
			biggestNews = articles[0].Title
			if articles[0].Link != "" {
				biggestNews = fmt.Sprintf("<a href=\"%s\">%s</a>", articles[0].Link, escapeHTML(articles[0].Title))
			} else {
				biggestNews = escapeHTML(articles[0].Title)
			}
		}

		var upcomingEarnings []string
		for _, symbol := range u.Symbols {
			info, err := yahoo.FetchEarnings(symbol)
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if info.EarningsDate.After(now) && info.EarningsDate.Before(nextWeek) {
				upcomingEarnings = append(upcomingEarnings, fmt.Sprintf("%s (%s)", symbol, info.EarningsDate.Format("Jan 02")))
			}
			time.Sleep(200 * time.Millisecond)
		}

		if len(perfs) == 0 && biggestNews == "" && len(upcomingEarnings) == 0 {
			continue
		}

		var sb strings.Builder
		sb.WriteString("<b>Your watchlist this week</b>\n\n")

		if len(perfs) > 0 {
			best := perfs[0]
			worst := perfs[0]
			var sum float64
			for _, p := range perfs {
				if p.pct > best.pct {
					best = p
				}
				if p.pct < worst.pct {
					worst = p
				}
				sum += p.pct
			}
			avg := sum / float64(len(perfs))
			sb.WriteString(fmt.Sprintf("• Best: <b>%s</b> %+.1f%%\n", best.symbol, best.pct))
			sb.WriteString(fmt.Sprintf("• Worst: <b>%s</b> %+.1f%%\n", worst.symbol, worst.pct))
			sb.WriteString(fmt.Sprintf("• Avg move: %+.1f%%\n", avg))
		}
		if biggestNews != "" {
			sb.WriteString(fmt.Sprintf("\n• Biggest news: %s\n", biggestNews))
		}
		if len(upcomingEarnings) > 0 {
			sb.WriteString(fmt.Sprintf("\n• Upcoming: %s\n", strings.Join(upcomingEarnings, ", ")))
		}

		s.notify(u.ChatID, sb.String())
		time.Sleep(100 * time.Millisecond)
	}

	log.Println("Weekly recap: sent")
}

// buildDigest constructs an HTML digest message. If summariser is set, uses AI summaries per company.
func (s *Scheduler) buildDigest(symbolArticles map[string][]storage.CachedArticle) string {
	if len(symbolArticles) == 0 {
		return ""
	}

	var sb strings.Builder
	if s.summariser != nil {
		sb.WriteString("<b>News digest — last 4 hours</b>\n")
	} else {
		sb.WriteString("<b>New headlines for your watchlist</b>\n")
	}

	totalArticles := 0
	for symbol, articles := range symbolArticles {
		if len(articles) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("\n<b>%s</b>\n", symbol))

		if s.summariser != nil {
			headlines := make([]string, 0, len(articles))
			for _, a := range articles {
				headlines = append(headlines, a.Title)
			}
			summary, err := s.summariser.SummarizeHeadlines(symbol, headlines)
			if err != nil {
				log.Printf("SummarizeHeadlines %s: %v", symbol, err)
				// Fall back to raw headlines
				for i := 0; i < len(articles) && i < 5; i++ {
					a := articles[i]
					sb.WriteString(fmt.Sprintf("• <a href=\"%s\">%s</a>\n", a.Link, escapeHTML(a.Title)))
				}
			} else {
				sb.WriteString(escapeHTML(summary))
				sb.WriteString("\n")
				// Include top 2 headline links for context
				for i := 0; i < len(articles) && i < 2; i++ {
					a := articles[i]
					sb.WriteString(fmt.Sprintf("• <a href=\"%s\">%s</a>\n", a.Link, escapeHTML(a.Title)))
				}
			}
			time.Sleep(300 * time.Millisecond) // Rate limit AI calls
		} else {
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
