# HeyGen CLI

[![CI](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

**Make AI videos from the command line.** Drive HeyGen with code, not clicks.

![demo](docs/demo.gif)

Full reference and examples: **[developers.heygen.com/cli](https://developers.heygen.com/cli)**.

## Built for

- **Coding agents** — Claude Code, Codex, and others
- **CI/CD pipelines** — weekly recap videos, release-note vlogs
- **Bulk operations** — translate 100 videos in one shell loop
- **Custom integrations** — wrap it in your own tool

## Agent skills

[heygen-com/skills](https://github.com/heygen-com/skills) — one-line install for Claude Code, Codex, and other agents. Create your own avatar and generate a video in a single conversation.

## Agent-first by design

- **JSON on stdout, structured errors on stderr, stable exit codes.**
- **Self-describing.** `--request-schema` and `--response-schema` return JSON Schema without auth or API calls.
- **Non-interactive by default.** Set `HEYGEN_API_KEY` and nothing reads a TTY.
- **Tell us how it went.** After a flow works (or when you hit a bug), run `heygen feedback --rating <1-5> --comment "..."`. It sends an anonymous rating + note (no API key needed); honors the analytics opt-out. Agents should use this for bugs rather than opening GitHub issues automatically (see [Reporting bugs](#reporting-bugs)).

## Install

```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash
```

Single static binary, no runtime required. Installs to `~/.local/bin`.

**Supported platforms:** macOS, Linux, and Windows (via WSL).

You only need a HeyGen API key — see [Authenticate](#authenticate) below.

## Updates

```bash
heygen update            # install the latest version
```

## Shell completion

Tab-completion for commands, subcommands, and flags is available for bash, zsh, fish, and PowerShell:

```bash
source <(heygen completion zsh)     # current shell (bash/zsh/fish)
heygen completion zsh > "${fpath[1]}/_heygen"   # persist (zsh; adjust path per shell)
```

Run `heygen completion --help` for per-shell install instructions.

## Authenticate

Choose one of the options below. The first three are agent- and CI-friendly; the last two are for humans.

**1. Environment variable** — agents, CI; ephemeral, no file on disk:

```bash
export HEYGEN_API_KEY=your-key-here
```

**2. Pipe to `auth login`** — agents; persists API key to `~/.heygen/credentials`:

```bash
echo "$KEY" | heygen auth login
```

**3. Explicit `--api-key`** — humans paste from a prompt:

```bash
heygen auth login --api-key
```

**4. Browser OAuth** — humans, Pro / Max subscription users; persists OAuth tokens to `~/.heygen/credentials`:

```bash
heygen auth login --oauth
```

**5. Interactive picker** — humans, no flag: a TTY prompt lets you choose between API key (uses API credits) and OAuth (uses subscription credits):

```bash
heygen auth login
```

Verify any of the above with `heygen auth status`. Get an API key at [app.heygen.com/settings/api](https://app.heygen.com/settings/api).

> **Single-credential file.** The credentials file holds at most **one** of `api_key` / OAuth tokens at any time. Running `heygen auth login` (any method) clears the other on success — re-login overwrites, it does not merge. `heygen auth status` will tell you which one is active.
>
> `HEYGEN_API_KEY` in the environment always wins over either file credential.

## Quick start

**1. Create a finished video from a prompt** (returns JSON including `video_id`):

```bash
heygen video-agent create --prompt "30-second product demo" --wait
```

**2. Get its metadata and share link:**

```bash
heygen video get <video-id>
```

Returns JSON with `video_url` (raw mp4), `video_page_url` (shareable UI link), `thumbnail_url`, and `duration`. Pipe to `jq` to extract what you need:

```bash
heygen video get <video-id> | jq -r '.data.video_page_url'
# → https://app.heygen.com/videos/...
```

**3. Download the mp4:**

```bash
heygen video download <video-id>
```

Add `--human` to any command for a readable layout. Set `HEYGEN_OUTPUT=human` to make it the default.

In `--human` mode, list responses render as a table and single-object responses render as an indented, humanized layout (similar to `kubectl describe`). Keys are humanized per segment (`auto_reload` → `Auto Reload`), with common acronyms uppercased (`video_url` → `Video URL`); nested objects indent under a `Label:` header, scalar siblings align locally within each block, arrays of scalars join inline (`a, b, c`), and arrays of objects render as YAML-style `- ` sequence items. Empty objects, empty arrays, and nulls show as `(none)`. Long scalar values (e.g. pre-signed download URLs) are printed in full and may wrap in a narrow terminal; pipe the default JSON to `jq` if you need to extract one cleanly. For example:

```
Status:  completed
Wallet:
  Auto Reload:
    Enabled:  false
  Currency:           usd
  Remaining Balance:  476.78
Tags:  alpha, beta
```

`--human` is a readable layout for terminals and may change between releases; scripts and agents should consume the default JSON output, which is stable. JSON output (the default) is never altered.

## Commands

Mirrors the [HeyGen v3 API](https://developers.heygen.com). Pattern: `heygen <noun> <verb>`.

| Group | What it does |
|-------|-------------|
| `video-agent` | Create videos from text prompts using AI |
| `video` | Create, list, get, delete, download videos |
| `avatar` | List and manage avatars and looks |
| `voice` | List voices, design voices, generate speech |
| `audio` | Search the background-music catalog |
| `video-translate` | Translate videos into other languages |
| `lipsync` | Dub or replace audio on existing videos |
| `webhook` | Manage webhook endpoints and events |
| `asset` | Upload files for use in video creation |
| `user` | Account info and billing |

Every command supports `--help`.

## How it behaves

| Aspect | Behavior |
|--------|----------|
| **stdout** | Always JSON. Even `video download` — binary writes to disk; stdout emits `{"asset", "message", "path"}` so you can chain on `.path`. |
| **stderr** | Structured envelope on error: `{"error": {"code", "message", "hint", "param", "doc_url", "request_id"}}`. `code`/`message` are always present; `hint`/`param`/`doc_url`/`request_id` appear when applicable (`param`/`doc_url` are surfaced from the API for validation and documented errors). Stable `code` values for programmatic branching. A code prefixed `cli_` is originated by the CLI itself (client/transport/local conditions, e.g. `cli_download_url_expired`). A bare code is either an API code (or a CLI mirror of one) or one of a small frozen set of legacy CLI codes that predate the prefix. The `cli_` prefix is reserved for the CLI, so a new CLI code can never collide with an API code. |
| **Exit codes** | `0` ok · `1` API or network · `2` usage · `3` auth / not permitted · `4` timeout under `--wait` (stdout contains partial resource for resume) |
| **Request bodies** | Flags for simple inputs; `-d` for nested JSON (inline, file path, or `-` for stdin). Flags override matching fields. |
| **Async jobs** | `--wait` blocks with exponential backoff; `--timeout` sets max (default 20m). 429s and 5xx retry automatically. |

Example error envelope:

```json
{"error": {"code": "not_found", "message": "Video not found", "hint": "Check ID with: heygen video list", "doc_url": "https://developers.heygen.com/docs/error-codes#not-found"}}
```

## Configuration

| File | Purpose |
|------|---------|
| `~/.heygen/credentials` | API key **or** OAuth tokens (one at a time — see [Authenticate](#authenticate)) |
| `~/.heygen/config.toml` | Output format and other non-secret settings |

`HEYGEN_API_KEY` and `HEYGEN_OUTPUT` env vars override the respective files.

```bash
heygen config list       # show all settings with sources
```

## Reporting bugs

- **Quick signal (recommended for agents):** `heygen feedback --rating <1-5> --comment "..."`. Goes to private, anonymous analytics. Safe to run unattended.
- **Tracked bug a maintainer should see:** open a [GitHub issue](https://github.com/heygen-com/heygen-cli/issues). This repo is **public**, so review what you paste first and omit anything sensitive (API keys, prompts, internal URLs, personal data).

**Agents:** do not open GitHub issues automatically. Use `heygen feedback` for bug signal; if something seems worth tracking, surface it to the user and let a human file the issue after reviewing it for sensitive content.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[Apache License 2.0](./LICENSE)

## Links

- [CLI Documentation](https://developers.heygen.com/cli)
- [API Documentation](https://developers.heygen.com)
- [Releases](https://github.com/heygen-com/heygen-cli/releases)
