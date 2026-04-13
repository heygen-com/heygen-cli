# HeyGen CLI

[![CI](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

**Make AI videos from the command line.** Drive HeyGen with code, not clicks.

![demo](docs/demo.gif)

Full reference and examples: **[developers.heygen.com/cli](https://developers.heygen.com/cli)**.

## Built for

- **Coding agents** — Claude Code, Codex, and others. Tool-use is trivial when every command is JSON-in, JSON-out with schemas you can introspect.
- **CI/CD pipelines** — auto-generate weekly recap videos, keep release-note vlogs in sync with commits.
- **Bulk operations** — translate 100 videos in one shell loop; dub a back-catalog overnight.
- **Custom integrations** — wrap it in your own tool; the contract (exit codes, error envelope, JSON output) is stable.

## Agent-first by design

- **JSON on stdout, structured errors on stderr, stable exit codes** (`0` ok, `1` API, `2` usage, `3` auth, `4` timeout).
- **Self-describing.** `--request-schema` and `--response-schema` return JSON Schema without auth or API calls.
- **Non-interactive by default.** Set `HEYGEN_API_KEY` and nothing reads a TTY.
- **`auth login` is the one exception** — it reads stdin, and accepts a pipe, so agents can wire it up non-interactively too.

## Install

```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash
```

Installs to `~/.local/bin`. macOS + Linux; Windows via WSL. See the [CLI docs](https://developers.heygen.com/cli) for other install methods.

## Authenticate

Three options. The first two are agent- and CI-friendly; the third is for humans.

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

## Commands

Mirrors the [HeyGen v3 API](https://developers.heygen.com). Pattern: `heygen <noun> <verb>`.

| Group | What it does |
|-------|-------------|
| `video-agent` | Create videos from text prompts using AI |
| `video` | Create, list, get, delete, download videos |
| `avatar` | List and manage avatars and looks |
| `voice` | List voices, design voices, generate speech |
| `video-translate` | Translate videos into other languages |
| `lipsync` | Dub or replace audio on existing videos |
| `webhook` | Manage webhook endpoints and events |
| `asset` | Upload files for use in video creation |
| `user` | Account info and billing |

Every command supports `--help`. Commands with a request body expose `--request-schema`; commands with a structured response expose `--response-schema`.

## How it behaves

**Stdout is always JSON.** Even `video download` — the file writes to disk and stdout gets `{"asset", "message", "path"}` so agents can chain on `.path`.

**Stderr on error** is a structured envelope:

```json
{"error": {"code": "not_found", "message": "Video not found", "hint": "Check ID with: heygen video list"}}
```

**Exit codes:** `0` success · `1` API or network error · `2` usage error · `3` auth · `4` timeout under `--wait` (stdout contains the partial resource so you can resume).

**Request bodies.** Simple fields go through flags. Nested inputs use `-d` (inline JSON, a file path, or `-` for stdin). Flags override fields in the JSON body.

**Async jobs.** Use `--wait` to block with exponential backoff; `--timeout` controls the max wait (default 20m). 429s and 5xx are retried automatically.

## Configuration

Credentials: `~/.heygen/credentials`. Config: `~/.heygen/config.toml`. `HEYGEN_API_KEY` and `HEYGEN_OUTPUT` env vars override both.

```bash
heygen config list       # show all settings with sources
heygen update            # self-update to latest
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

[Apache License 2.0](./LICENSE)

## Links

- [CLI Documentation](https://developers.heygen.com/cli)
- [API Documentation](https://developers.heygen.com)
- [Releases](https://github.com/heygen-com/heygen-cli/releases)
