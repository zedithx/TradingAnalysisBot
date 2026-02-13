package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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
	Articles map[string][]CachedArticle `json:"articles"` // symbol -> cached articles
}

// Store is a concurrency-safe, JSON-file-backed storage for all users.
type Store struct {
	mu       sync.RWMutex
	filePath string
	Users    map[string]*UserData `json:"users"` // chatID (string) -> UserData
}

// New creates or loads a Store from the given data directory.
func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	fp := filepath.Join(dataDir, "users.json")
	s := &Store{
		filePath: fp,
		Users:    make(map[string]*UserData),
	}

	// Load existing data if the file exists
	data, err := os.ReadFile(fp)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &s.Users); err != nil {
			return nil, fmt.Errorf("parse users.json: %w", err)
		}
	}

	return s, nil
}

// save writes the current state to disk. Must be called with mu held.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.Users, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0o644); err != nil {
		return fmt.Errorf("write users.json: %w", err)
	}
	return nil
}

// getOrCreateUser returns the UserData for the given chat ID, creating it if needed.
// Must be called with mu held for writing.
func (s *Store) getOrCreateUser(chatID int64) *UserData {
	key := fmt.Sprintf("%d", chatID)
	u, ok := s.Users[key]
	if !ok {
		u = &UserData{
			ChatID:   chatID,
			Symbols:  []string{},
			AddedAt:  time.Now().UTC(),
			Articles: make(map[string][]CachedArticle),
		}
		s.Users[key] = u
	}
	if u.Articles == nil {
		u.Articles = make(map[string][]CachedArticle)
	}
	return u
}

// AddSymbol adds a stock symbol to the user's watchlist.
// Returns an error if the symbol is already present.
func (s *Store) AddSymbol(chatID int64, symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	u := s.getOrCreateUser(chatID)

	for _, sym := range u.Symbols {
		if sym == symbol {
			return fmt.Errorf("%s is already in your watchlist", symbol)
		}
	}

	u.Symbols = append(u.Symbols, symbol)
	return s.save()
}

// RemoveSymbol removes a stock symbol from the user's watchlist.
func (s *Store) RemoveSymbol(chatID int64, symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	u := s.getOrCreateUser(chatID)

	found := false
	filtered := make([]string, 0, len(u.Symbols))
	for _, sym := range u.Symbols {
		if sym == symbol {
			found = true
			continue
		}
		filtered = append(filtered, sym)
	}

	if !found {
		return fmt.Errorf("%s is not in your watchlist", symbol)
	}

	u.Symbols = filtered
	delete(u.Articles, symbol)
	return s.save()
}

// GetSymbols returns the user's current watchlist.
func (s *Store) GetSymbols(chatID int64) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%d", chatID)
	u, ok := s.Users[key]
	if !ok {
		return nil
	}
	result := make([]string, len(u.Symbols))
	copy(result, u.Symbols)
	return result
}

// GetAllUsers returns all user data (used by the background fetcher).
func (s *Store) GetAllUsers() map[string]*UserData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*UserData, len(s.Users))
	for k, v := range s.Users {
		result[k] = v
	}
	return result
}

// AddArticles appends new articles for a user/symbol pair, deduplicating by link.
// Keeps at most 50 articles per symbol (most recent), auto-pruning older ones.
// Returns the slice of newly added (non-duplicate) articles.
func (s *Store) AddArticles(chatID int64, symbol string, articles []CachedArticle) ([]CachedArticle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.getOrCreateUser(chatID)

	// Build a set of existing links for fast dedup
	existing := make(map[string]bool, len(u.Articles[symbol]))
	for _, a := range u.Articles[symbol] {
		existing[a.Link] = true
	}

	var added []CachedArticle
	for _, a := range articles {
		if existing[a.Link] {
			continue
		}
		u.Articles[symbol] = append(u.Articles[symbol], a)
		existing[a.Link] = true
		added = append(added, a)
	}

	// Sort by published date descending (newest first)
	sort.Slice(u.Articles[symbol], func(i, j int) bool {
		return u.Articles[symbol][i].Published.After(u.Articles[symbol][j].Published)
	})

	// Keep at most 50 per symbol
	if len(u.Articles[symbol]) > 50 {
		u.Articles[symbol] = u.Articles[symbol][:50]
	}

	return added, s.save()
}

// GetLatestArticles returns the N most recent cached articles for a user.
// If symbol is empty, returns articles across all watchlist symbols.
// Results are sorted newest-first.
func (s *Store) GetLatestArticles(chatID int64, symbol string, limit int) []CachedArticle {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%d", chatID)
	u, ok := s.Users[key]
	if !ok {
		return nil
	}

	var all []CachedArticle

	if symbol != "" {
		// Single symbol
		all = append(all, u.Articles[symbol]...)
	} else {
		// All symbols
		for _, articles := range u.Articles {
			all = append(all, articles...)
		}
	}

	// Sort newest first
	sort.Slice(all, func(i, j int) bool {
		return all[i].Published.After(all[j].Published)
	})

	// Deduplicate by link (in case the same article appears under multiple symbols)
	seen := make(map[string]bool)
	deduped := make([]CachedArticle, 0, len(all))
	for _, a := range all {
		if seen[a.Link] {
			continue
		}
		seen[a.Link] = true
		deduped = append(deduped, a)
	}

	if limit > 0 && len(deduped) > limit {
		deduped = deduped[:limit]
	}

	return deduped
}

// PruneOldArticles removes cached articles older than 7 days for all users.
func (s *Store) PruneOldArticles() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)

	for _, u := range s.Users {
		for sym, articles := range u.Articles {
			filtered := make([]CachedArticle, 0, len(articles))
			for _, a := range articles {
				if a.Published.After(cutoff) {
					filtered = append(filtered, a)
				}
			}
			u.Articles[sym] = filtered
		}
	}

	return s.save()
}
