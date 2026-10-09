# maitake

A multi-user proxy that serves Claude Code and Codex (ChatGPT) subscriptions as Claude Messages, OpenAI Chat Completions and OpenAI Responses APIs, with a built-in admin dashboard. Inspired by [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).

- Per-user API keys with allowed models, rate limits and expiry
- Account pool with priorities, automatic failover, cooldowns and token refresh
- Live quota tracking for every account, including Codex banked resets (view, redeem, auto-redeem)
- Usage logging per user, key, model and account
- A read-only quota page for users, with admin-defined display profiles filtered server-side
- Upcoming and predicted Codex resets from [codex-resets.com](https://codex-resets.com)

## Endpoints

| Path | Format |
| --- | --- |
| `POST /v1/messages` | Claude Messages (Claude and Codex models) |
| `POST /v1/messages/count_tokens` | Claude token counting |
| `POST /v1/chat/completions` | OpenAI Chat Completions (Claude and Codex models) |
| `POST /v1/responses` | OpenAI Responses (Claude and Codex models); `GET` upgrades to the WebSocket transport |
| `POST /v1/responses/input_tokens` | Responses token counting |
| `POST /v1/responses/compact` | Codex history compaction |
| `POST /v1/images/generations`, `/v1/images/edits` | OpenAI Images, drawn by Codex's image tool |
| `/backend-api/codex/responses[/compact]`, `/models`, `/alpha/search` | Codex CLI's native paths |
| `POST /v1/alpha/search` | Codex CLI web search |
| `GET /v1/models?client_version=…` | Model list in the Codex CLI's format, Claude models included |
| `GET /v1/models` | Model list, filtered by the key's allowed models |

Append a reasoning level to any model, e.g. `gpt-5.5(high)` or `claude-opus-5(max)`.

## Layout

```
cmd/maitake/        entrypoint
internal/accounts/  account pool: selection, refresh, cooldowns, quota polling
internal/provider/  Claude and Codex logins, request shaping, quota parsing
internal/translate/ format translation between Chat, Messages and Responses
internal/proxy/     /v1 API
internal/quota/     quota snapshots and display-profile views
internal/resets/    codex-resets.com poller
internal/server/    admin API, auth, SPA serving
internal/store/     SQLite storage and migrations
web/                SvelteKit dashboard (SPA, embedded into the binary)
```

## Development

Backend (port 8317):

```sh
go run ./cmd/maitake
```

Dashboard (port 3000, proxies `/api` to the backend):

```sh
cd web
pnpm install
pnpm dev
```

## Build

```sh
pnpm -C web build
go build -tags embedui -o maitake ./cmd/maitake
```

Without `-tags embedui` the binary serves only the API.

## First run

On first start the server logs a one-time setup token. Open the dashboard, paste it, and create the admin account.

## Configuration

Copy `config.example.yaml` to `config.yaml`. It only holds bootstrap settings (listen address, trusted proxies, login rate limit, outbound proxy); everything else is managed from the dashboard.
