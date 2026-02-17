package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration values for the application.
type Config struct {
	TelegramBotToken string
	OpenAIAPIKey     string
	SupabaseURL      string
	AnalyseWhitelist map[int64]bool
}

// Load reads configuration from environment variables (with .env fallback).
func Load() (*Config, error) {
	// Load .env file if it exists; ignore error if missing
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL environment variable is required")
	}

	whitelist := parseWhitelist(os.Getenv("ANALYSE_WHITELIST"))

	return &Config{
		TelegramBotToken: token,
		OpenAIAPIKey:     openaiKey,
		SupabaseURL:      supabaseURL,
		AnalyseWhitelist: whitelist,
	}, nil
}

// parseWhitelist converts a comma-separated string of user IDs into a lookup map.
func parseWhitelist(raw string) map[int64]bool {
	result := make(map[int64]bool)
	if raw == "" {
		return result
	}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			result[id] = true
		}
	}
	return result
}
