# Complete Guide: from Install to Running

🇬🇧 English · [🇮🇩 Bahasa Indonesia](PANDUAN.md) · [README](README.en.md)

This guide takes you from nothing to a running, usable AI Gateway.

## 1. Requirements

- **Go 1.22 or newer.** Check with `go version`.
- At least **one API key** from an AI provider (some are free, see step 3).
- A terminal: Terminal (macOS/Linux), PowerShell (Windows), or the Firebase Studio terminal.

## 2. Install Go

| System | How |
|---|---|
| Windows / macOS | Download from https://go.dev/dl and run the installer. Open a new terminal. |
| Linux (Debian/Ubuntu) | Download the tarball from https://go.dev/dl, then `sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go*.tar.gz` and add `export PATH=$PATH:/usr/local/go/bin` to `~/.bashrc`. |
| Nix / NixOS | `nix-shell -p go` (choose the `go` package, not `gccgo` or `go_1_21`). |
| Firebase Studio | Add `pkgs.go` to `packages` in `.idx/dev.nix`, then *Rebuild environment*. |

Verify:

```bash
go version      # must be go1.22 or higher
```

If you see `C compiler "gcc" not found`, run once: `go env -w CGO_ENABLED=0` (this project is pure Go and does not need gcc).

## 3. Get API keys (one or more)

| Provider | Cost | Get it at |
|---|---|---|
| Gemini | Free (rate limited) | https://aistudio.google.com/apikey |
| Groq | Free (rate limited) | https://console.groq.com/keys |
| OpenRouter | Free for `:free` models | https://openrouter.ai/keys |
| Mistral | Free (rate limited) | https://console.mistral.ai |
| Claude / GPT | Paid | Optional, used as a last-resort fallback |

Even with no keys, the Custom AI (ASCII images, forecasts from a price series you send, answers from trained data) still works. For general questions you need at least one key.

## 4. Get the code

```bash
# from the zip
unzip ai-gateway.zip && cd ai-gateway

# or from GitHub
git clone https://github.com/USERNAME/REPO.git && cd REPO
```

Make sure you are in the folder that contains `go.mod`.

## 5. Configure (`.env`)

```bash
cp .env.example .env
```

Open `.env` in an editor and fill it in. Example:

```
GATEWAY_KEY=a-long-secret-you-make-up
GEMINI_API_KEYS=your_gemini_key
GROQ_API_KEYS=your_groq_key
```

Formatting rules (a frequent source of errors):
- **No spaces** around `=`. Right: `GROQ_API_KEYS=gsk_xxx`. Wrong: `GROQ_API_KEYS= gsk_xxx`.
- **No comments** on the same line as a key.
- **Only the key.** Do not write the word `Bearer` in front of it.
- Several keys for one provider are comma-separated: `GEMINI_API_KEYS=key1,key2`.
- `GATEWAY_KEY` is the password for this API (make it up). Anyone who knows it can use your API.
- **Never** put keys in `.env.example` or commit `.env` to GitHub.

## 6. Run

```bash
go run ./cmd/server
```

Success looks like `AI gateway aktif di :8080` (or `127.0.0.1:8080`). Lines such as `peringatan: provider "..." dilewati` only mean that provider has no key yet, which is normal.
Leave this terminal open. Press `Ctrl+C` to stop.

Different port: `PORT=9000 go run ./cmd/server` (Windows PowerShell: `$env:PORT=9000; go run ./cmd/server`).

## 7. Try it (in a second terminal)

Store `GATEWAY_KEY` in a variable so you do not retype it:

```bash
# macOS / Linux
export GATEWAY_KEY=a-long-secret-you-make-up
```

```powershell
# Windows PowerShell
$env:GATEWAY_KEY="a-long-secret-you-make-up"
```

**Health check**

```bash
curl localhost:8080/healthz
# {"status":"ok"}
```

**Ask the AI**

```bash
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"Hello, answer in one sentence"}'
```

The response contains `text` (the answer), `provider` (who answered), and `failed_attempts` (who failed before that).

**Train, then ask**

```bash
curl -X POST localhost:8080/v1/train -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"question":"What are the shop hours?","answer":"Open 08:00-17:00 every day."}'

curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"what are the shop hours?"}'
```

**Custom AI features**

```bash
# image request -> ASCII art (add "in binary" for a 0/1 pattern)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"draw a cat"}'

# market forecast -> "text" + "secondary" (formulas)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"forecast bitcoin price 30 days"}'
```

**Check key and provider status**

```bash
curl localhost:8080/v1/status -H "Authorization: Bearer $GATEWAY_KEY"
```

Windows users: in PowerShell `curl` is a different alias. Use `curl.exe`, or:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/v1/chat `
  -Headers @{Authorization="Bearer $env:GATEWAY_KEY"} `
  -Body '{"prompt":"Hello"}'
```

## 8. Use it from other apps

The API is OpenAI-compatible. Python example (`pip install openai`):

```python
from openai import OpenAI
c = OpenAI(base_url="http://localhost:8080/v1", api_key="YOUR_GATEWAY_KEY")
r = c.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hello"}])
print(r.choices[0].message.content)
```

`model` can be `auto`, a provider name (`gemini`), or `provider/model` (`groq/openai/gpt-oss-20b`).

## 9. Change models or provider order

Model names change often. To change one without touching code, use env vars in `.env`:

```
GEMINI_MODEL=gemini-3.8-flash
GROQ_MODEL=openai/gpt-oss-20b
OPENROUTER_MODEL=openrouter/free
```

To change the order, copy `config.example.json` to `config.json` and edit each `priority` (lower = tried first). To see which models your Groq key can use:

```bash
curl https://api.groq.com/openai/v1/models -H "Authorization: Bearer YOUR_GROQ_KEY"
```

Restart the server after editing `.env` or `config.json`.

## 10. Run the tests

```bash
go vet ./...
go test ./...
```

## 11. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `401 API key gateway salah/tidak ada` | The `Authorization: Bearer ...` header does not match `GATEWAY_KEY` in `.env`. Make sure your shell variable is not empty (`echo ${#GATEWAY_KEY}` must be > 0). |
| `503 all providers failed` | Read the `attempts` list. Each entry gives that provider's reason (below). |
| `HTTP 404 model ... not found` / `no longer available` | The model name is gone or your account has no access. Change it via `<NAME>_MODEL` (step 9). |
| `HTTP 401` from one provider | Wrong/revoked key, or the word `Bearer`, a space, or a comment ended up in `.env`. |
| `HTTP 429` | Free quota used up or rate-limited. The gateway fails over automatically; wait a few minutes. |
| `HTTP 503 ... high demand` | The provider is busy (temporary). The gateway fails over automatically. |
| `tidak ada provider aktif` (no active provider) | No key was read. Check that `.env` is in the same folder you run `go run` from. |
| `connection refused` | The server is not running. Run `go run ./cmd/server` and leave it open. |
| `command not found` on `source .env` | There is a space after `=` in `.env`. Fix the format (step 5). The Go server itself does not need `source`. |
| `address already in use` | Port is taken. Use `PORT=8081 go run ./cmd/server`. |
| `gcc not found` | `go env -w CGO_ENABLED=0`. |
| On Firebase Studio, the `cloudworkstations.dev` URL redirects (302) to a login | Normal; that URL is protected by Google login. Test via `localhost:8080` in the terminal, or via the Web Preview panel. |
| Custom AI keeps "declining" | Normal for new questions; the gateway moves on to another AI and learns its answer. |

## 12. Security (must read)

- Set `GATEWAY_KEY` before exposing the service to the internet.
- Never paste the contents of `.env` or terminal output containing keys into chats, issues or forums. If you already did, regenerate that key at the provider and delete the old one.
- Never commit `.env`. `.gitignore` already excludes it; verify with `git status`.
- If GitHub rejects a push because of a "secret", remove the key from the commit history (do not just add another commit) and rotate the key.
- The Custom AI memory is shared by all gateway users; do not send personal data.

## 13. Next steps

- Deploy to the internet: Vercel or Netlify (see `README.en.md`, Deploy sections). Training data there must use Upstash.
- Custom AI details (options, limits, privacy): the "Custom AI" section of `README.en.md`.
