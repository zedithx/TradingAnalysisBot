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
-- UPDATE users SET first_used_at = COALESCE(first_used_at, added_at) WHERE first_used_at IS NULL;

-- Trial reminder tracking (columns are auto-created on app startup if missing)

-- Alert triggers: per-user, per-symbol, per-type deduplication (created on app startup if missing)
