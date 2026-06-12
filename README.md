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

## Authenticate

Choose one of the three options below. The first two are agent- and CI-friendly; the third is for humans.

**1. Environment variable** — agents, CI; ephemeral, no file on disk:

```bash
export HEYGEN_API_KEY=your-key-here
```

**2. Pipe to `auth login`** — agents; persists to `~/.heygen/credentials`:

```bash
echo "$KEY" | heygen auth login
```

**3. Interactive `auth login`** — humans; persists to `~/.heygen/credentials`:

```bash
heygen auth login
```

Verify any of the above with `heygen auth status`. Get a key at [app.heygen.com/settings/api](https://app.heygen.com/settings/api).

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

In `--human` mode, list responses render as a table and single-object responses render as aligned key-value rows. Nested objects are flattened into dotted keys (e.g. `wallet.auto_reload.enabled: false`); arrays of scalars are joined inline (`a, b, c`) and arrays of objects are indexed (`messages.0.role`). JSON output (the default) is never altered.

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
| **stderr** | Structured envelope on error: `{"error": {"code", "message", "hint"}}`. Stable `code` values for programmatic branching. |
| **Exit codes** | `0` ok · `1` API or network · `2` usage · `3` auth · `4` timeout under `--wait` (stdout contains partial resource for resume) |
| **Request bodies** | Flags for simple inputs; `-d` for nested JSON (inline, file path, or `-` for stdin). Flags override matching fields. |
| **Async jobs** | `--wait` blocks with exponential backoff; `--timeout` sets max (default 20m). 429s and 5xx retry automatically. |

Example error envelope:

```json
{"error": {"code": "not_found", "message": "Video not found", "hint": "Check ID with: heygen video list"}}
```

## Configuration

| File | Purpose |
|------|---------|
| `~/.heygen/credentials` | API key |
| `~/.heygen/config.toml` | Output format and other non-secret settings |

`HEYGEN_API_KEY` and `HEYGEN_OUTPUT` env vars override the respective files.

```bash
heygen config list       # show all settings with sources
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[Apache License 2.0](./LICENSE)

## Links

- [CLI Documentation](https://developers.heygen.com/cli)
- [API Documentation](https://developers.heygen.com)
- [Releases](https://github.com/heygen-com/heygen-cli/releases)
