// Migrate reads data/users.json and uploads it to Supabase.
// Run: go run ./scripts/migrate (from project root)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type CachedArticle struct {
	Symbol    string    `json:"symbol"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Published time.Time `json:"published"`
	FetchedAt time.Time `json:"fetched_at"`
}

type UserData struct {
	ChatID   int64                      `json:"chat_id"`
	Symbols  []string                   `json:"symbols"`
	AddedAt  time.Time                  `json:"added_at"`
	Articles map[string][]CachedArticle `json:"articles"`
}

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")

	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		log.Fatal("SUPABASE_URL not set")
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	usersFile := filepath.Join(dataDir, "users.json")

	data, err := os.ReadFile(usersFile)
	if err != nil {
		log.Fatalf("Read %s: %v", usersFile, err)
	}

	var users map[string]*UserData
	if err := json.Unmarshal(data, &users); err != nil {
		log.Fatalf("Parse JSON: %v", err)
	}

	if len(users) == 0 {
		log.Println("No users in file. Nothing to migrate.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, supabaseURL)
	if err != nil {
		log.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	// Ensure schema exists (idempotent)
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS users (chat_id BIGINT PRIMARY KEY, added_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS symbols (id SERIAL PRIMARY KEY, chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE, symbol TEXT NOT NULL, UNIQUE(chat_id, symbol))`,
		`CREATE TABLE IF NOT EXISTS articles (id SERIAL PRIMARY KEY, chat_id BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE, symbol TEXT NOT NULL, title TEXT NOT NULL, link TEXT NOT NULL, published TIMESTAMPTZ NOT NULL, fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(chat_id, symbol, link))`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			log.Fatalf("Schema: %v", err)
		}
	}

	usersInserted, symbolsInserted, articlesInserted := 0, 0, 0

	for _, u := range users {
		if u == nil {
			continue
		}

		// Insert user
		_, err := pool.Exec(ctx, `INSERT INTO users (chat_id, added_at) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET added_at = EXCLUDED.added_at`, u.ChatID, u.AddedAt)
		if err != nil {
			log.Printf("Insert user %d: %v", u.ChatID, err)
			continue
		}
		usersInserted++

		// Insert symbols
		for _, sym := range u.Symbols {
			_, err := pool.Exec(ctx, `INSERT INTO symbols (chat_id, symbol) VALUES ($1, $2) ON CONFLICT (chat_id, symbol) DO NOTHING`, u.ChatID, sym)
			if err != nil {
				log.Printf("Insert symbol %s for %d: %v", sym, u.ChatID, err)
				continue
			}
			symbolsInserted++
		}

		// Insert articles
		for sym, arts := range u.Articles {
			for _, a := range arts {
				_, err := pool.Exec(ctx,
					`INSERT INTO articles (chat_id, symbol, title, link, published, fetched_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (chat_id, symbol, link) DO NOTHING`,
					u.ChatID, sym, a.Title, a.Link, a.Published, a.FetchedAt,
				)
				if err != nil {
					log.Printf("Insert article for %d %s: %v", u.ChatID, sym, err)
					continue
				}
				articlesInserted++
			}
		}
	}

	fmt.Printf("\nMigration done: %d users, %d symbols, %d articles\n", usersInserted, symbolsInserted, articlesInserted)
}
