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

// WhitelistStatus stores whitelist flags for a user/chat.
type WhitelistStatus struct {
	ChatID     int64
	Analyse    bool
	Watchlist  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	store.ensureSwingTraderTables(ctx)
	store.ensureWhitelistTable(ctx)
	return store, nil
}

// ensureTrialReminderColumns adds trial reminder and slots-expansion columns if they don't exist.
func (s *Store) ensureTrialReminderColumns(ctx context.Context) {
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_5d_sent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_1d_sent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_reminder_expired_sent BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS slots_expanded_until TIMESTAMPTZ`,
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
	if _, err := s.pool.Exec(ctx, `ALTER TABLE alert_triggers ADD COLUMN IF NOT EXISTS trigger_value DOUBLE PRECISION`); err != nil {
		log.Printf("ensureAlertTriggersTable add trigger_value: %v", err)
	}
}

// ensureSwingTraderTables creates user_preferences, price_alerts, earnings_reminders, digest_article_state.
func (s *Store) ensureSwingTraderTables(ctx context.Context) {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS user_preferences (
			chat_id BIGINT PRIMARY KEY REFERENCES users(chat_id) ON DELETE CASCADE,
			digest_frequency_hours INT DEFAULT 4,
			alert_frequency_hours INT DEFAULT 2,
			dnd_start_utc TIME,
			dnd_end_utc TIME,
			timezone TEXT,
			last_digest_sent_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS price_alerts (
			id SERIAL PRIMARY KEY,
			chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			type TEXT NOT NULL,
			threshold DOUBLE PRECISION NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(chat_id, symbol, type)
		)`,
		`CREATE TABLE IF NOT EXISTS earnings_reminders (
			chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (chat_id, symbol)
		)`,
		`CREATE TABLE IF NOT EXISTS digest_article_state (
			chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			article_link TEXT NOT NULL,
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			delta_label TEXT,
			PRIMARY KEY (chat_id, symbol, article_link)
		)`,
		`CREATE TABLE IF NOT EXISTS analyse_usage (
			user_id BIGINT NOT NULL,
			usage_date DATE NOT NULL,
			count INT NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, usage_date)
		)`,
	} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			log.Printf("ensureSwingTraderTables: %v", err)
		}
	}
	// Migrate: add alert_frequency_hours column if missing.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS alert_frequency_hours INT DEFAULT 2`); err != nil {
		log.Printf("ensureSwingTraderTables add alert_frequency_hours: %v", err)
	}
}

// ensureWhitelistTable creates the user_whitelists table if it doesn't exist.
func (s *Store) ensureWhitelistTable(ctx context.Context) {
	_, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS user_whitelists (
			chat_id BIGINT PRIMARY KEY,
			analyse_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			watchlist_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		log.Printf("ensureWhitelistTable: %v", err)
	}
}

// RecordAlertTrigger upserts the alert trigger timestamp for deduplication.
func (s *Store) RecordAlertTrigger(chatID int64, symbol, triggerType string) error {
	return s.RecordAlertTriggerValue(chatID, symbol, triggerType, nil)
}

// RecordAlertTriggerValue upserts the alert trigger timestamp and optional value for deduplication and re-trigger logic.
func (s *Store) RecordAlertTriggerValue(chatID int64, symbol, triggerType string, triggerValue *float64) error {
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO alert_triggers (chat_id, symbol, trigger_type, triggered_at, trigger_value)
		 VALUES ($1, $2, $3, NOW(), $4)
		 ON CONFLICT (chat_id, symbol, trigger_type)
		 DO UPDATE SET triggered_at = NOW(), trigger_value = EXCLUDED.trigger_value`,
		chatID, symbol, triggerType, triggerValue)
	return err
}

// GetAlertTriggerValue returns the last recorded trigger value for an alert trigger.
func (s *Store) GetAlertTriggerValue(chatID int64, symbol, triggerType string) (float64, bool, error) {
	var triggerValue *float64
	err := s.pool.QueryRow(context.Background(),
		`SELECT trigger_value
		 FROM alert_triggers
		 WHERE chat_id = $1 AND symbol = $2 AND trigger_type = $3`,
		chatID, symbol, triggerType,
	).Scan(&triggerValue)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	if triggerValue == nil {
		return 0, false, nil
	}
	return *triggerValue, true, nil
}

// WasAlertTriggeredWithin returns true if this alert was triggered within the given duration.
func (s *Store) WasAlertTriggeredWithin(chatID int64, symbol, triggerType string, within time.Duration) (bool, error) {
	if within <= 0 {
		return false, nil
	}

	var triggeredAt time.Time
	err := s.pool.QueryRow(context.Background(),
		`SELECT triggered_at
		 FROM alert_triggers
		 WHERE chat_id = $1 AND symbol = $2 AND trigger_type = $3`,
		chatID, symbol, triggerType,
	).Scan(&triggeredAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return time.Since(triggeredAt.UTC()) < within, nil
}

// WasAlertTriggeredInLast24h returns true if this alert was triggered in the last 24 hours.
func (s *Store) WasAlertTriggeredInLast24h(chatID int64, symbol, triggerType string) (bool, error) {
	return s.WasAlertTriggeredWithin(chatID, symbol, triggerType, 24*time.Hour)
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

// GetWhitelistStatus returns database-backed whitelist flags for a chat.
func (s *Store) GetWhitelistStatus(chatID int64) (WhitelistStatus, error) {
	var status WhitelistStatus
	status.ChatID = chatID

	err := s.pool.QueryRow(context.Background(),
		`SELECT chat_id, analyse_enabled, watchlist_enabled, created_at, updated_at
		 FROM user_whitelists
		 WHERE chat_id = $1`,
		chatID,
	).Scan(&status.ChatID, &status.Analyse, &status.Watchlist, &status.CreatedAt, &status.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return status, nil
		}
		return status, err
	}

	return status, nil
}

// AddWhitelistFlags enables whitelist flags for a chat without disabling existing flags.
func (s *Store) AddWhitelistFlags(chatID int64, analyse, watchlist bool) error {
	if !analyse && !watchlist {
		return nil
	}

	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO user_whitelists (chat_id, analyse_enabled, watchlist_enabled, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET
		   analyse_enabled = user_whitelists.analyse_enabled OR EXCLUDED.analyse_enabled,
		   watchlist_enabled = user_whitelists.watchlist_enabled OR EXCLUDED.watchlist_enabled,
		   updated_at = NOW()`,
		chatID, analyse, watchlist,
	)
	return err
}

// RemoveWhitelistFlags disables selected whitelist flags for a chat.
func (s *Store) RemoveWhitelistFlags(chatID int64, analyse, watchlist bool) error {
	if !analyse && !watchlist {
		return nil
	}

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE user_whitelists
		 SET analyse_enabled = CASE WHEN $2 THEN FALSE ELSE analyse_enabled END,
		     watchlist_enabled = CASE WHEN $3 THEN FALSE ELSE watchlist_enabled END,
		     updated_at = NOW()
		 WHERE chat_id = $1`,
		chatID, analyse, watchlist,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM user_whitelists
		 WHERE chat_id = $1
		   AND NOT analyse_enabled
		   AND NOT watchlist_enabled`,
		chatID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ListWhitelists returns all active database-backed whitelist entries.
func (s *Store) ListWhitelists() ([]WhitelistStatus, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT chat_id, analyse_enabled, watchlist_enabled, created_at, updated_at
		 FROM user_whitelists
		 WHERE analyse_enabled OR watchlist_enabled
		 ORDER BY chat_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []WhitelistStatus
	for rows.Next() {
		var status WhitelistStatus
		if err := rows.Scan(&status.ChatID, &status.Analyse, &status.Watchlist, &status.CreatedAt, &status.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, status)
	}
	return result, rows.Err()
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

// HasSlotsExpanded returns true if the user has an active slots expansion (20 instead of 10).
func (s *Store) HasSlotsExpanded(chatID int64) (bool, error) {
	ctx := context.Background()
	var until *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT slots_expanded_until FROM users WHERE chat_id = $1`,
		chatID,
	).Scan(&until)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if until == nil {
		return false, nil
	}
	return until.After(time.Now().UTC()), nil
}

// ExtendSlotsExpansion adds 30 days to the user's slots expansion (20 watchlist slots).
func (s *Store) ExtendSlotsExpansion(chatID int64) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET slots_expanded_until = GREATEST(COALESCE(slots_expanded_until, NOW()), NOW()) + INTERVAL '30 days'
		 WHERE chat_id = $1`,
		chatID,
	)
	return err
}

// TrialReminderCandidate holds user data for trial reminder decisions.
type TrialReminderCandidate struct {
	ChatID              int64
	FirstUsedAt         time.Time
	Reminder5dSent      bool
	Reminder1dSent      bool
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

// UserPreferences holds digest, alert, and DND settings.
type UserPreferences struct {
	ChatID               int64
	DigestFrequencyHours int // 2, 4, 8, or 24
	AlertFrequencyHours  int // 1, 2, 4, or 8 — how often price alerts can fire
	DNDStartUTC          *time.Time
	DNDEndUTC            *time.Time
	Timezone             string
	LastDigestSentAt     *time.Time
}

// GetPreferences returns user preferences, or defaults (4h digest, 2h alerts) if none.
func (s *Store) GetPreferences(chatID int64) (*UserPreferences, error) {
	ctx := context.Background()
	var p UserPreferences
	var dndStart, dndEnd interface{}
	var lastSent interface{}
	err := s.pool.QueryRow(ctx,
		`SELECT chat_id, COALESCE(digest_frequency_hours, 4), COALESCE(alert_frequency_hours, 2), dnd_start_utc, dnd_end_utc,
		 COALESCE(timezone, ''), last_digest_sent_at
		 FROM user_preferences WHERE chat_id = $1`,
		chatID,
	).Scan(&p.ChatID, &p.DigestFrequencyHours, &p.AlertFrequencyHours, &dndStart, &dndEnd, &p.Timezone, &lastSent)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &UserPreferences{ChatID: chatID, DigestFrequencyHours: 4, AlertFrequencyHours: 2}, nil
		}
		return nil, err
	}
	// Parse TIME columns as time.Time (UTC date + time)
	if dndStart != nil {
		if t, ok := dndStart.(time.Time); ok {
			p.DNDStartUTC = &t
		}
	}
	if dndEnd != nil {
		if t, ok := dndEnd.(time.Time); ok {
			p.DNDEndUTC = &t
		}
	}
	if lastSent != nil {
		if t, ok := lastSent.(time.Time); ok {
			p.LastDigestSentAt = &t
		}
	}
	return &p, nil
}

// SetPreferences upserts user preferences.
func (s *Store) SetPreferences(chatID int64, freqHours int, dndStart, dndEnd *time.Time, timezone string) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	if freqHours != 2 && freqHours != 4 && freqHours != 8 && freqHours != 24 {
		freqHours = 4
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_preferences (chat_id, digest_frequency_hours, dnd_start_utc, dnd_end_utc, timezone, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET
		 digest_frequency_hours = $2, dnd_start_utc = $3, dnd_end_utc = $4, timezone = $5, updated_at = NOW()`,
		chatID, freqHours, dndStart, dndEnd, timezone,
	)
	return err
}

// SetDigestFrequency sets digest frequency (2, 4, 8, 24 hours).
func (s *Store) SetDigestFrequency(chatID int64, hours int) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	if hours != 2 && hours != 4 && hours != 8 && hours != 24 {
		hours = 4
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_preferences (chat_id, digest_frequency_hours, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET digest_frequency_hours = $2, updated_at = NOW()`,
		chatID, hours,
	)
	return err
}

// SetAlertFrequency sets price alert frequency (1, 2, 4, or 8 hours).
func (s *Store) SetAlertFrequency(chatID int64, hours int) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	if hours != 1 && hours != 2 && hours != 4 && hours != 8 {
		hours = 2
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_preferences (chat_id, alert_frequency_hours, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET alert_frequency_hours = $2, updated_at = NOW()`,
		chatID, hours,
	)
	return err
}

// GetAlertCooldown returns the per-user price alert cooldown duration.
func (s *Store) GetAlertCooldown(chatID int64) time.Duration {
	prefs, err := s.GetPreferences(chatID)
	if err != nil || prefs.AlertFrequencyHours <= 0 {
		return 2 * time.Hour
	}
	return time.Duration(prefs.AlertFrequencyHours)*time.Hour - 5*time.Minute // subtract 5min for scheduling jitter
}

// SetLastDigestSentAt records when the last digest was sent.
func (s *Store) SetLastDigestSentAt(chatID int64, t time.Time) error {
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO user_preferences (chat_id, last_digest_sent_at, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (chat_id) DO UPDATE SET last_digest_sent_at = $2, updated_at = NOW()`,
		chatID, t,
	)
	return err
}

// IsInDND returns true if the current UTC time falls within the user's DND window.
func (s *Store) IsInDND(chatID int64, now time.Time) (bool, error) {
	p, err := s.GetPreferences(chatID)
	if err != nil || p.DNDStartUTC == nil || p.DNDEndUTC == nil {
		return false, err
	}
	// Compare time-of-day (UTC)
	nowTime := time.Date(0, 1, 1, now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
	start := time.Date(0, 1, 1, p.DNDStartUTC.Hour(), p.DNDStartUTC.Minute(), p.DNDStartUTC.Second(), 0, time.UTC)
	end := time.Date(0, 1, 1, p.DNDEndUTC.Hour(), p.DNDEndUTC.Minute(), p.DNDEndUTC.Second(), 0, time.UTC)
	if start.Before(end) {
		return !nowTime.Before(start) && nowTime.Before(end), nil
	}
	// DND spans midnight
	return !nowTime.Before(start) || nowTime.Before(end), nil
}

// PriceAlert represents a user-defined price alert.
type PriceAlert struct {
	ID        int64
	ChatID    int64
	Symbol    string
	Type      string // "pct" or "price"
	Threshold float64
	CreatedAt time.Time
}

// GetPriceAlerts returns all price alerts for a user.
func (s *Store) GetPriceAlerts(chatID int64) ([]PriceAlert, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, chat_id, symbol, type, threshold, created_at FROM price_alerts WHERE chat_id = $1 ORDER BY symbol, type`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PriceAlert
	for rows.Next() {
		var a PriceAlert
		if err := rows.Scan(&a.ID, &a.ChatID, &a.Symbol, &a.Type, &a.Threshold, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// GetPriceAlertSymbols returns distinct symbols that have at least one price alert.
func (s *Store) GetPriceAlertSymbols() ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT DISTINCT symbol FROM price_alerts ORDER BY symbol`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, err
		}
		result = append(result, sym)
	}
	return result, rows.Err()
}

// GetPriceAlertsForSymbol returns price alerts for a given symbol (all users tracking it).
func (s *Store) GetPriceAlertsForSymbol(symbol string) ([]PriceAlert, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, chat_id, symbol, type, threshold, created_at FROM price_alerts WHERE symbol = $1`,
		symbol,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PriceAlert
	for rows.Next() {
		var a PriceAlert
		if err := rows.Scan(&a.ID, &a.ChatID, &a.Symbol, &a.Type, &a.Threshold, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// AddPriceAlert adds a price alert. type: "pct" or "price".
func (s *Store) AddPriceAlert(chatID int64, symbol, alertType string, threshold float64) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if alertType != "pct" && alertType != "price" {
		return fmt.Errorf("alert type must be pct or price")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO price_alerts (chat_id, symbol, type, threshold)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (chat_id, symbol, type) DO UPDATE SET threshold = $4`,
		chatID, symbol, alertType, threshold,
	)
	return err
}

// RemovePriceAlert removes a price alert by ID or by (chat_id, symbol, type).
func (s *Store) RemovePriceAlert(chatID int64, symbol, alertType string) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	_, err := s.pool.Exec(context.Background(),
		`DELETE FROM price_alerts WHERE chat_id = $1 AND symbol = $2 AND type = $3`,
		chatID, symbol, alertType,
	)
	return err
}

// IsEarningsReminderEnabled returns true if reminder is enabled for (chatID, symbol). Default true if no row.
func (s *Store) IsEarningsReminderEnabled(chatID int64, symbol string) (bool, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	var enabled bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT enabled FROM earnings_reminders WHERE chat_id = $1 AND symbol = $2`,
		chatID, symbol,
	).Scan(&enabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return true, nil
		}
		return false, err
	}
	return enabled, nil
}

// GetDigestArticleLinks returns article links previously sent in digest for (chatID, symbol).
func (s *Store) GetDigestArticleLinks(chatID int64, symbol string) (map[string]bool, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT article_link FROM digest_article_state WHERE chat_id = $1 AND symbol = $2`,
		chatID, symbol,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			return nil, err
		}
		result[link] = true
	}
	return result, rows.Err()
}

// RecordDigestArticles records article links sent in a digest for delta labeling.
func (s *Store) RecordDigestArticles(chatID int64, symbol string, articles []CachedArticle) error {
	ctx := context.Background()
	for _, a := range articles {
		_, err := s.pool.Exec(ctx,
			`INSERT INTO digest_article_state (chat_id, symbol, article_link, last_seen_at, delta_label)
			 VALUES ($1, $2, $3, NOW(), 'new')
			 ON CONFLICT (chat_id, symbol, article_link) DO UPDATE SET last_seen_at = NOW(), delta_label = 'unchanged'`,
			chatID, symbol, a.Link,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetAnalyseUsage returns the current daily usage count for a user. Does NOT increment.
func (s *Store) GetAnalyseUsage(userID int64, maxPerDay int) (count int, remaining int, err error) {
	today := time.Now().UTC().Format("2006-01-02")
	err = s.pool.QueryRow(context.Background(),
		`SELECT count FROM analyse_usage WHERE user_id = $1 AND usage_date = $2::date`,
		userID, today,
	).Scan(&count)
	if err == pgx.ErrNoRows {
		return 0, maxPerDay, nil
	}
	if err != nil {
		return 0, 0, err
	}
	remaining = maxPerDay - count
	if remaining < 0 {
		remaining = 0
	}
	return count, remaining, nil
}

// ConsumeAnalyseSlot atomically increments daily usage if under limit. Returns (allowed, remaining, err).
func (s *Store) ConsumeAnalyseSlot(userID int64, maxPerDay int) (allowed bool, remaining int, err error) {
	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err = s.pool.QueryRow(context.Background(),
		`INSERT INTO analyse_usage (user_id, usage_date, count)
		 VALUES ($1, $2::date, 1)
		 ON CONFLICT (user_id, usage_date)
		 DO UPDATE SET count = analyse_usage.count + 1
		 WHERE analyse_usage.count < $3
		 RETURNING count`,
		userID, today, maxPerDay,
	).Scan(&count)
	if err == pgx.ErrNoRows {
		// At limit — no row updated
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, maxPerDay - count, nil
}

// SetEarningsReminder enables or disables earnings reminder for a symbol.
func (s *Store) SetEarningsReminder(chatID int64, symbol string, enabled bool) error {
	ctx := context.Background()
	if err := s.ensureUser(ctx, chatID); err != nil {
		return err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO earnings_reminders (chat_id, symbol, enabled, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (chat_id, symbol) DO UPDATE SET enabled = $3, updated_at = NOW()`,
		chatID, symbol, enabled,
	)
	return err
}
