# TradingNewsBot — Your Market Intelligence in Telegram

**Stop refreshing Yahoo Finance. Get news, earnings dates, and AI analysis for your watchlist directly in Telegram.**

[![Telegram](https://img.shields.io/badge/Telegram-TradingNewsBot-0088cc?logo=telegram)](https://t.me/TradingNewsBot)

---

## What It Does

TradingNewsBot consolidates everything you need to track your stocks:

- **Watchlist** — Add stocks by ticker or snap a photo of your broker's watchlist (AI extracts tickers)
- **News** — AI-summarized digest every 4 hours for each company in your watchlist
- **Earnings Dates** — Next report dates so you never miss Q1/Q2/Q3/Q4
- **Earnings Reminders** — 1 day before a stock reports, we remind you
- **AI Analysis** — Sentiment, key risks, and short-term outlook from recent news + live data
- **Global Markets** — US (AAPL), Korea (005930.KS), HK (0700.HK), Singapore (D05.SI), etc.

No app switching. No dashboards. Just open Telegram.

---

## Commands

| Command | What it does |
|---------|--------------|
| `/start` | Get started |
| `/add` | Add ticker or send a photo of your watchlist |
| `/remove SYMBOL` | Remove from watchlist |
| `/list` | View your watchlist |
| `/news` | Latest headlines (watchlist or specific ticker) |
| `/reports` | Next earnings dates for your watchlist |
| `/analyse` | AI analysis for one stock or your whole watchlist |
| `/help` | Command list |

---

## Pricing

- **First 30 days free**
- **100 Telegram Stars/month** after that (pay in-app, no credit card)
- Cancel anytime

---

## Get Started

1. Open Telegram
2. Search for **@TradingNewsBot** (or your bot's username)
3. Send `/start`
4. Add your first stock with `/add AAPL` or send a screenshot of your watchlist

---

## Why Traders Use It

- **Photo watchlist** — Screenshot your broker, we OCR it. No typing tickers one by one.
- **News in one place** — AI-summarized digests every 4 hours so you're not hunting across 5 sites.
- **Never miss earnings** — Daily reminders 1 day before reports.
- **AI that's actually useful** — Summarizes sentiment and risks, not generic fluff.
- **Works in Telegram** — Where you already are. No new app to check.

---

## Disclaimer

This bot is for informational and educational purposes only. Nothing provided constitutes financial advice. Always do your own research and consult a qualified professional before making investment decisions.

---

## Technical

- **Data**: Yahoo Finance (news, quotes, earnings)
- **AI**: GPT-4o-mini (vision for OCR, chat for analysis)
- **Storage**: Supabase (Postgres)
- **Hosting**: Google Cloud Run
