-- Run this in your Supabase SQL Editor (Dashboard > SQL Editor > New query)
-- This creates the tables needed by TradingNewsBot.

-- Users table
CREATE TABLE IF NOT EXISTS users (
    chat_id          BIGINT PRIMARY KEY,
    added_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    first_used_at    TIMESTAMPTZ DEFAULT NOW(),
    subscribed_until TIMESTAMPTZ
);

-- Watchlist symbols per user
CREATE TABLE IF NOT EXISTS symbols (
    id        SERIAL PRIMARY KEY,
    chat_id   BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
    symbol    TEXT NOT NULL,
    UNIQUE(chat_id, symbol)
);

CREATE INDEX IF NOT EXISTS idx_symbols_chat_id ON symbols(chat_id);

-- Cached news articles per user/symbol
CREATE TABLE IF NOT EXISTS articles (
    id         SERIAL PRIMARY KEY,
    chat_id    BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    title      TEXT NOT NULL,
    link       TEXT NOT NULL,
    published  TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(chat_id, symbol, link)
);

CREATE INDEX IF NOT EXISTS idx_articles_chat_symbol ON articles(chat_id, symbol);
CREATE INDEX IF NOT EXISTS idx_articles_published   ON articles(published DESC);

-- Subscription paywall migration (run if users table existed before paywall):
-- ALTER TABLE users ADD COLUMN IF NOT EXISTS first_used_at TIMESTAMPTZ DEFAULT NOW();
-- ALTER TABLE users ADD COLUMN IF NOT EXISTS subscribed_until TIMESTAMPTZ;
-- ALTER TABLE users ADD COLUMN IF NOT EXISTS slots_expanded_until TIMESTAMPTZ;
-- UPDATE users SET first_used_at = COALESCE(first_used_at, added_at) WHERE first_used_at IS NULL;

-- Trial reminder tracking (columns are auto-created on app startup if missing)

-- Alert triggers: per-user, per-symbol, per-type deduplication (created on app startup if missing)

-- Swing Trader Phase 1: User preferences (digest frequency, DND, timezone)
CREATE TABLE IF NOT EXISTS user_preferences (
    chat_id                BIGINT PRIMARY KEY REFERENCES users(chat_id) ON DELETE CASCADE,
    digest_frequency_hours INT DEFAULT 4 CHECK (digest_frequency_hours IN (2, 4, 8, 24)),
    dnd_start_utc          TIME,
    dnd_end_utc            TIME,
    timezone               TEXT,
    last_digest_sent_at    TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- User-defined price alerts (percentage or price level)
CREATE TABLE IF NOT EXISTS price_alerts (
    id         SERIAL PRIMARY KEY,
    chat_id    BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('pct', 'price')),
    threshold  DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(chat_id, symbol, type)
);

CREATE INDEX IF NOT EXISTS idx_price_alerts_chat_id ON price_alerts(chat_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_symbol ON price_alerts(chat_id, symbol);

-- Per-ticker earnings reminder opt-in/opt-out (default: enabled if no row)
CREATE TABLE IF NOT EXISTS earnings_reminders (
    chat_id    BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, symbol)
);

CREATE INDEX IF NOT EXISTS idx_earnings_reminders_chat_id ON earnings_reminders(chat_id);

-- Digest article state for delta labeling (Phase 5)
CREATE TABLE IF NOT EXISTS digest_article_state (
    chat_id      BIGINT NOT NULL REFERENCES users(chat_id) ON DELETE CASCADE,
    symbol       TEXT NOT NULL,
    article_link TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delta_label  TEXT CHECK (delta_label IN ('new', 'improving', 'worsening', 'unchanged')),
    PRIMARY KEY (chat_id, symbol, article_link)
);

CREATE INDEX IF NOT EXISTS idx_digest_article_state_chat ON digest_article_state(chat_id, symbol);

-- Daily /analyse usage limit (5 per user per day UTC)
CREATE TABLE IF NOT EXISTS analyse_usage (
    user_id    BIGINT NOT NULL,
    usage_date DATE NOT NULL,
    count      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, usage_date)
);

-- Database-backed whitelist flags for admin overrides
CREATE TABLE IF NOT EXISTS user_whitelists (
    chat_id           BIGINT PRIMARY KEY,
    analyse_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    watchlist_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
