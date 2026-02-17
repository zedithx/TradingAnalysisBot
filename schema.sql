-- Run this in your Supabase SQL Editor (Dashboard > SQL Editor > New query)
-- This creates the tables needed by TradingNewsBot.

-- Users table
CREATE TABLE IF NOT EXISTS users (
    chat_id   BIGINT PRIMARY KEY,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
