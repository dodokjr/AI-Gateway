# AI Gateway (Go) — Claude + GPT + Gemini + more, text only

🇬🇧 English · [🇮🇩 Bahasa Indonesia](README.md)

Complete guide from install to running: [GUIDE.md](GUIDE.md)

One API for many AI providers. When a key or provider runs out of tokens/quota (429/402),
the gateway automatically switches to the next key/provider. It can be "trained" through a
knowledge base. Text only: images, video and audio are rejected (HTTP 400).

The gateway core has no external dependencies (Go standard library only); only the Netlify
function uses `aws-lambda-go`.

## Run

```bash
cp .env.example .env        # fill in at least one API key
go run ./cmd/server
# optional: cp config.example.json config.json to change provider order/models
```

Providers without a key are skipped automatically. They are tried in `priority` order (lowest first).
Default order: Custom AI > Gemini > Groq > OpenRouter > Mistral (free) > Claude > GPT (paid, last-resort fallback).
For a fully free setup with no quota, run Ollama locally and set `"disabled": false` on the `ollama` provider.

## How failover works

- Multiple keys per provider: `GEMINI_API_KEYS=key1,key2,key3` (rotated round-robin).
- 429 / 402 (quota/balance exhausted) -> the key enters a cooldown (1 minute, or 1 hour for daily quotas) and the gateway moves to another key/provider.
- 401/403 -> the key rests for 6 hours. 5xx / timeout -> 30 seconds.
- Check the current state: `GET /v1/status`.

## Endpoints

Everything except `/healthz` needs the header `Authorization: Bearer $GATEWAY_KEY`.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/chat` | Simple chat (`prompt` or `messages`) |
| POST | `/v1/chat/completions` | OpenAI-compatible format (works with OpenAI SDKs) |
| POST | `/v1/train` | Add knowledge (`question`,`answer`) or `items:[...]` |
| GET | `/v1/train` | List knowledge (`?q=` to search) |
| DELETE | `/v1/train/{id}` | Delete knowledge |
| POST | `/v1/feedback` | Rate `1`/`-1` on a knowledge item (`id`), or save a good answer |
| GET/PUT | `/v1/persona` | Set a fixed personality/instruction |
| GET | `/v1/status` | Provider and key status |

### Examples

```bash
# Train
curl -X POST localhost:8080/v1/train -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"question":"What are the shop hours?","answer":"Open 08:00-17:00 every day."}'

# Ask (relevant knowledge is injected automatically)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"When does the shop open?"}'

# Prefer a specific provider (still fails over if it errors)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"Hello","provider":"claude"}'
```

On `/v1/chat/completions`, set `model` to `auto`, a provider name (`gemini`),
or `provider/model` (`groq/openai/gpt-oss-20b`).

## About "training"

This is **not** fine-tuning of model weights (that cannot be done for free across providers).
The gateway stores question-answer examples in `data/knowledge.json`, finds the 3 most
relevant ones for each question, and injects them into the prompt. The result applies to
every provider and improves as data and feedback grow.

## Deploy to Vercel

1. Push this folder to GitHub, then *Add New Project* in Vercel and pick the repo (or use the `vercel` CLI).
2. Under *Settings > Environment Variables* set: `GATEWAY_KEY`, provider API keys (e.g. `GEMINI_API_KEYS`, `GROQ_API_KEYS`), and Upstash (`UPSTASH_REDIS_REST_URL`, `UPSTASH_REDIS_REST_TOKEN`).
3. Deploy. Test: `curl https://NAME.vercel.app/healthz`.

`vercel.json` already routes `/v1/*` to the function `api/index.go` (maxDuration 60 s).

## Deploy to Netlify

1. Once, on your own computer, create the function's `go.sum`: `cd netlify/functions/api && go mod tidy`, then commit it.
2. Push to GitHub, then *Add new site > Import from Git* in Netlify.
3. Set *Site configuration > Environment variables* the same way as on Vercel.
4. Deploy. Test: `curl https://NAME.netlify.app/healthz`.

Netlify's default function timeout is 10 s (can be raised up to 26 s in site settings). On Netlify the gateway uses 8 s per attempt and 24 s in total; if you do not raise the timeout, set `GATEWAY_TIMEOUT_SEC=4` and `GATEWAY_TOTAL_TIMEOUT_SEC=9`.

## Important on serverless

- **Training data requires Upstash.** The Vercel/Netlify disk is temporary; without Upstash, training data is lost when an instance is recycled (`GET /v1/status` shows `"storage": "ephemeral"` or `"upstash"`).
- Key cooldown state is kept per instance (in memory), so each instance may occasionally retry a key that is already exhausted. This is safe; it only adds one failed attempt.
- Free Upstash holds roughly 900 KB of training data.
- Other variable: `CONFIG_JSON` (the full config.json content, overrides the default).

## Custom AI (local engine, priority 1)

A provider of type `custom` (`gateway/custom/`) is tried first. It is **not a generative language model**:
its answers come from trained memory, the knowledge base (`/v1/train`), web search results, and formulas.
When its confidence is below `threshold` (default 0.8) it "declines" the request, the gateway moves on to
another AI (with no cooldown for the Custom AI), passes the web search context along to that AI, and the
Custom AI then **learns that AI's answer** for next time.

| File | Contents |
|---|---|
| `custom_provider.go` | Engine, confidence threshold, dual answers, `Declined` (failover trigger) |
| `math_engine.go` | TF-IDF, Cosine, Levenshtein, Softmax, EMA, RSI, GBM, Monte Carlo, Black-Scholes |
| `market_data.go` | Price data: Stooq (stocks/indices/forex/commodities) and CoinGecko (crypto) |
| `web_search.go` | DuckDuckGo (no key), Google Programmable Search, Brave Search |
| `learning.go` | Self-learning memory, TTL, personal/real-time data filter |
| `ascii_art.go` | Image requests -> monochrome ASCII art or a 0/1 binary pattern |

Examples:

```bash
# market forecast -> "text" (main answer) + "secondary" (formula breakdown)
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"forecast bitcoin price 30 days"}'
# image request -> ASCII / binary
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"draw a cat in binary"}'
# give feedback on a memory answer (id is in meta.memory_id); -1 twice = no longer used
curl -sX POST localhost:8080/v1/feedback -H "Authorization: Bearer $GATEWAY_KEY" -d '{"id":"ID","rating":-1}'
```

Options live in `config.json` -> provider `custom` -> `options`: `threshold`, `web_search`, `search_engines`
(`duckduckgo`, `google`, `brave`), `max_results`, `simulations`, `learn_from_peers`, `peer_ttl_days`, `market`.

Important limits:
- **Memory matching is word-based and deliberately strict.** It cannot tell "opening hours" from "closing hours"
  or "Blue Shop" from "Red Shop", so only (nearly) identical questions are answered from memory. Lowering
  `threshold` produces more wrong answers.
- **Memory is shared by all gateway users.** Questions containing emails, long numbers or secrets, and real-time
  data (today, latest, prices), are not learned. Still, avoid sending personal data.
- **Privacy:** questions not found in memory are sent to a search engine (DuckDuckGo by default). Set `"web_search": false` to disable.
- Make sure your AI providers' terms allow storing their answers; disable with `"learn_from_peers": false`.
- Google/Bing results pages are not scraped (violates their terms and gets blocked quickly). The Bing Search API has been retired; use Brave instead.
- Market forecasts are statistical projections from historical data, not financial advice.
- On serverless, the learning memory needs Upstash to persist (same as the knowledge base).

## Notes

- Free quotas and model names change from time to time; adjust them in `config.json`.
- Set `GATEWAY_KEY` before exposing the service to the internet. Never commit `.env`.
- `stream` is not supported yet.
- Tests: `go test ./...`