package storage

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CachedArticle is a news article stored by the background fetcher.
type CachedArticle struct {
	Symbol    string    `json:"symbol"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Published time.Time `json:"published"`
	FetchedAt time.Time `json:"fetched_at"`
}

// UserData holds the watchlist and cached news for a single user.
type UserData struct {
	ChatID   int64                      `json:"chat_id"`
	Symbols  []string                   `json:"symbols"`
	AddedAt  time.Time                  `json:"added_at"`
	Articles map[string][]CachedArticle `json:"articles"`
}

// Store provides Supabase/Postgres-backed storage for all users.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to the Supabase Postgres database and returns a Store.
func New(databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{pool: pool}
	store.ensureTrialReminderColumns(ctx)
	store.ensureAlertTriggersTable(ctx)
	return store, nil
}

// ensureTrialReminderColumns adds trial reminder tracking columns if they don't exist.
func (s *Store) ensureTrialReminderColumns(ctx context.Context) {
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_5d_sent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_1d_sent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_expired_sent BOOLEAN DEFAULT FALSE`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			log.Printf("ensureTrialReminderColumns: %v", err)
		}
	}
}

// ensureAlertTriggersTable creates the alert_triggers table if it doesn't exist.
func (s *Store) ensureAlertTriggersTable(ctx context.Context) {
	_, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS alert_triggers (
			chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (chat_id, symbol, trigger_type)
		)`)
	if err != nil {
		log.Printf("ensureAlertTriggersTable: %v", err)
	}
}

// RecordAlertTrigger upserts the alert trigger timestamp for deduplication.
func (s *Store) RecordAlertTrigger(chatID int64, symbol, triggerType string) error {
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO alert_triggers (chat_id, symbol, trigger_type, triggered_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (chat_id, symbol, trigger_type)
		 DO UPDATE SET triggered_at = NOW()`,
		chatID, symbol, triggerType)
	return err
}

// WasAlertTriggeredInLast24h returns true if this alert was triggered in the last 24 hours.
func (s *Store) WasAlertTriggeredInLast24h(chatID int64, symbol, triggerType string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT EXISTS(
		 SELECT 1 FROM alert_triggers
		 WHERE chat_id = $1 AND symbol = $2 AND trigger_type = $3
		 AND triggered_at > NOW() - INTERVAL '24 hours'
		)`,
		chatID, symbol, triggerType).Scan(&ok)
	return ok, err
}

// Close releases the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// ensureUser creates a user row if it doesn't already exist, and sets first_used_at.
func (s *Store) ensureUser(ctx context.Context, chatID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (chat_id, first_used_at) VALUES ($1, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET first_used_at = COALESCE(users.first_used_at, NOW())`,
		chatID,
	)
	return err
}

// IsEligible returns true if the user is in their free period or has an active subscription.
func (s *Store) IsEligible(chatID int64) (bool, error) {
	ctx := context.Background()
	var firstUsedAt, subscribedUntil *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT first_used_at, subscribed_until FROM users WHERE chat_id = $1`,
		chatID,
	).Scan(&firstUsedAt, &subscribedUntil)
	if err != nil {
		if err == pgx.ErrNoRows {
			return true, nil // New user, not in DB yet — will be created and get free period
		}
		return false, err
	}

	now := time.Now().UTC()

	// Active subscription
	if subscribedUntil != nil && subscribedUntil.After(now) {
		return true, nil
	}

	// Free period: first 30 days from first_used_at
	if firstUsedAt != nil {
		freeEnd := firstUsedAt.Add(30 * 24 * time.Hour)
		if now.Before(freeEnd) {
			return true, nil
		}
	}

	return false, nil
}

// RecordFirstUse sets first_used_at if not already set (idempotent).
func (s *Store) RecordFirstUse(chatID int64) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (chat_id, first_used_at) VALUES ($1, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET first_used_at = COALESCE(users.first_used_at, NOW())`,
		chatID,
	)
	return err
}

// SetSubscribedUntil sets the user's subscription end date (for admin use).
func (s *Store) SetSubscribedUntil(chatID int64, until time.Time) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET subscribed_until = $2 WHERE chat_id = $1`,
		chatID, until,
	)
	return err
}

// ExtendSubscription adds 30 days to the user's subscription (from now or from existing end, whichever is later).
func (s *Store) ExtendSubscription(chatID int64) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET subscribed_until = GREATEST(COALESCE(subscribed_until, NOW()), NOW()) + INTERVAL '30 days'
		 WHERE chat_id = $1`,
		chatID,
	)
	return err
}

// TrialReminderCandidate holds user data for trial reminder decisions.
type TrialReminderCandidate struct {
	ChatID           int64
	FirstUsedAt      time.Time
	Reminder5dSent   bool
	Reminder1dSent   bool
	ReminderExpiredSent bool
}

// GetTrialReminderCandidates returns users on free trial (no active subscription) with reminder flags.
func (s *Store) GetTrialReminderCandidates() ([]TrialReminderCandidate, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT chat_id, first_used_at,
		 COALESCE(trial_reminder_5d_sent, FALSE),
		 COALESCE(trial_reminder_1d_sent, FALSE),
		 COALESCE(trial_reminder_expired_sent, FALSE)
		 FROM users
		 WHERE first_used_at IS NOT NULL
		 AND (subscribed_until IS NULL OR subscribed_until <= NOW())`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TrialReminderCandidate
	for rows.Next() {
		var c TrialReminderCandidate
		if err := rows.Scan(&c.ChatID, &c.FirstUsedAt, &c.Reminder5dSent, &c.Reminder1dSent, &c.ReminderExpiredSent); err != nil {
			return result, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// MarkTrialReminder5dSent marks the 5-day reminder as sent.
func (s *Store) MarkTrialReminder5dSent(chatID int64) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE users SET trial_reminder_5d_sent = TRUE WHERE chat_id = $1`, chatID)
	return err
}

// MarkTrialReminder1dSent marks the 1-day reminder as sent.
func (s *Store) MarkTrialReminder1dSent(chatID int64) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE users SET trial_reminder_1d_sent = TRUE WHERE chat_id = $1`, chatID)
	return err
}

// MarkTrialReminderExpiredSent marks the expired reminder as sent.
func (s *Store) MarkTrialReminderExpiredSent(chatID int64) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE users SET trial_reminder_expired_sent = TRUE WHERE chat_id = $1`, chatID)
	return err
}

// AddSymbol adds a stock symbol to the user's watchlist.
// Returns an error if the symbol is already present.
func (s *Store) AddSymbol(chatID int64, symbol string) error {
	ctx := context.Background()
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if err := s.ensureUser(ctx, chatID); err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO symbols (chat_id, symbol) VALUES ($1, $2)`,
		chatID, symbol,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return fmt.Errorf("%s is already in your watchlist", symbol)
		}
		return fmt.Errorf("add symbol: %w", err)
	}

	return nil
}

// RemoveSymbol removes a stock symbol from the user's watchlist.
func (s *Store) RemoveSymbol(chatID int64, symbol string) error {
	ctx := context.Background()
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	tag, err := s.pool.Exec(ctx,
		`DELETE FROM symbols WHERE chat_id = $1 AND symbol = $2`,
		chatID, symbol,
	)
	if err != nil {
		return fmt.Errorf("remove symbol: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s is not in your watchlist", symbol)
	}

	// Also remove cached articles for this symbol
	_, _ = s.pool.Exec(ctx,
		`DELETE FROM articles WHERE chat_id = $1 AND symbol = $2`,
		chatID, symbol,
	)

	return nil
}

// GetSymbols returns the user's current watchlist.
func (s *Store) GetSymbols(chatID int64) []string {
	ctx := context.Background()

	rows, err := s.pool.Query(ctx,
		`SELECT symbol FROM symbols WHERE chat_id = $1 ORDER BY id ASC`,
		chatID,
	)
	if err != nil {
		log.Printf("GetSymbols error: %v", err)
		return nil
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			log.Printf("GetSymbols scan error: %v", err)
			return nil
		}
		result = append(result, sym)
	}

	return result
}

// GetAllUsers returns all user data (used by the background fetcher).
func (s *Store) GetAllUsers() map[string]*UserData {
	ctx := context.Background()

	// Fetch all users
	userRows, err := s.pool.Query(ctx,
		`SELECT chat_id, added_at FROM users`,
	)
	if err != nil {
		log.Printf("GetAllUsers error: %v", err)
		return nil
	}
	defer userRows.Close()

	result := make(map[string]*UserData)
	for userRows.Next() {
		var chatID int64
		var addedAt time.Time
		if err := userRows.Scan(&chatID, &addedAt); err != nil {
			log.Printf("GetAllUsers scan error: %v", err)
			return nil
		}
		key := fmt.Sprintf("%d", chatID)
		result[key] = &UserData{
			ChatID:   chatID,
			Symbols:  []string{},
			AddedAt:  addedAt,
			Articles: make(map[string][]CachedArticle),
		}
	}

	// Fetch all symbols and attach to their users
	symRows, err := s.pool.Query(ctx,
		`SELECT chat_id, symbol FROM symbols ORDER BY id ASC`,
	)
	if err != nil {
		log.Printf("GetAllUsers symbols error: %v", err)
		return result
	}
	defer symRows.Close()

	for symRows.Next() {
		var chatID int64
		var symbol string
		if err := symRows.Scan(&chatID, &symbol); err != nil {
			log.Printf("GetAllUsers symbols scan error: %v", err)
			continue
		}
		key := fmt.Sprintf("%d", chatID)
		if ud, ok := result[key]; ok {
			ud.Symbols = append(ud.Symbols, symbol)
		}
	}

	return result
}

// AddArticles appends new articles for a user/symbol pair, deduplicating by link.
// Keeps at most 50 articles per symbol (most recent), auto-pruning older ones.
// Returns the slice of newly added (non-duplicate) articles.
func (s *Store) AddArticles(chatID int64, symbol string, articles []CachedArticle) ([]CachedArticle, error) {
	ctx := context.Background()

	if err := s.ensureUser(ctx, chatID); err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}

	// Get existing links for deduplication
	rows, err := s.pool.Query(ctx,
		`SELECT link FROM articles WHERE chat_id = $1 AND symbol = $2`,
		chatID, symbol,
	)
	if err != nil {
		return nil, fmt.Errorf("get existing links: %w", err)
	}

	existing := make(map[string]bool)
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan link: %w", err)
		}
		existing[link] = true
	}
	rows.Close()

	// Filter out duplicates
	var added []CachedArticle
	for _, a := range articles {
		if existing[a.Link] {
			continue
		}
		existing[a.Link] = true
		added = append(added, a)
	}

	// Batch insert new articles
	if len(added) > 0 {
		batch := &pgx.Batch{}
		for _, a := range added {
			batch.Queue(
				`INSERT INTO articles (chat_id, symbol, title, link, published, fetched_at)
				 VALUES ($1, $2, $3, $4, $5, $6)
				 ON CONFLICT (chat_id, symbol, link) DO NOTHING`,
				chatID, symbol, a.Title, a.Link, a.Published, a.FetchedAt,
			)
		}

		br := s.pool.SendBatch(ctx, batch)
		for range added {
			if _, err := br.Exec(); err != nil {
				log.Printf("AddArticles insert error: %v", err)
			}
		}
		br.Close()
	}

	// Prune to keep at most 50 per symbol (delete oldest beyond 50)
	_, _ = s.pool.Exec(ctx,
		`DELETE FROM articles WHERE id IN (
			SELECT id FROM articles
			WHERE chat_id = $1 AND symbol = $2
			ORDER BY published DESC
			OFFSET 50
		)`,
		chatID, symbol,
	)

	return added, nil
}

// GetLatestArticles returns the N most recent cached articles for a user.
// If symbol is empty, returns articles across all watchlist symbols.
// Results are sorted newest-first and deduplicated by link.
func (s *Store) GetLatestArticles(chatID int64, symbol string, limit int) []CachedArticle {
	ctx := context.Background()

	var rows pgx.Rows
	var err error

	// Fetch extra rows to account for deduplication across symbols
	queryLimit := limit * 3
	if queryLimit < 30 {
		queryLimit = 30
	}

	if symbol != "" {
		rows, err = s.pool.Query(ctx,
			`SELECT symbol, title, link, published, fetched_at
			 FROM articles
			 WHERE chat_id = $1 AND symbol = $2
			 ORDER BY published DESC
			 LIMIT $3`,
			chatID, symbol, queryLimit,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT symbol, title, link, published, fetched_at
			 FROM articles
			 WHERE chat_id = $1
			 ORDER BY published DESC
			 LIMIT $2`,
			chatID, queryLimit,
		)
	}

	if err != nil {
		log.Printf("GetLatestArticles error: %v", err)
		return nil
	}
	defer rows.Close()

	// Collect and deduplicate by link
	seen := make(map[string]bool)
	var result []CachedArticle
	for rows.Next() {
		var a CachedArticle
		if err := rows.Scan(&a.Symbol, &a.Title, &a.Link, &a.Published, &a.FetchedAt); err != nil {
			log.Printf("GetLatestArticles scan error: %v", err)
			return nil
		}
		if seen[a.Link] {
			continue
		}
		seen[a.Link] = true
		result = append(result, a)
	}

	// Sort newest first (should already be sorted, but ensure after dedup)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Published.After(result[j].Published)
	})

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result
}

// PruneOldArticles removes cached articles older than 7 days for all users.
func (s *Store) PruneOldArticles() error {
	ctx := context.Background()
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)

	_, err := s.pool.Exec(ctx,
		`DELETE FROM articles WHERE published < $1`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("prune old articles: %w", err)
	}

	return nil
}
