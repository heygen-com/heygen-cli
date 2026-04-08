# HeyGen CLI

Official command-line tool for the [HeyGen](https://heygen.com) video generation API. Create AI avatar videos, translate videos, generate speech, and manage assets — all from the terminal.

Built for developers, AI agents, and CI/CD pipelines. JSON output by default.

## Install

```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash
```

This installs the latest stable release into `~/.local/bin`.

<details>
<summary>Other install methods</summary>

**Homebrew** (once the repo is public):

```bash
brew install heygen/tap/heygen
```

**Specific version:**

```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash -s -- --version v0.1.0
```

**From source** (requires Go 1.23+):

```bash
git clone https://github.com/heygen-com/heygen-cli.git
cd heygen-cli && make install
```

</details>

## Authenticate

```bash
heygen auth login
```

Paste your API key when prompted. Get one from [app.heygen.com/settings/api](https://app.heygen.com/settings/api).

For CI/Docker/agents, set the environment variable instead:

```bash
export HEYGEN_API_KEY=your-key-here
```

Verify your credentials:

```bash
heygen auth status
```

## Quick Start

```bash
# Create a video from a text prompt (blocks until done)
heygen video-agent create --prompt "Make a 30-second product demo" --wait

# Download the result
heygen video download <video-id>

# List your videos
heygen video list --limit 5
```

## Commands

The CLI mirrors the [HeyGen v3 API](https://developers.heygen.com). Pattern: `heygen <noun> <verb>`.

| Group | What it does |
|-------|-------------|
| `video-agent` | Create videos from text prompts using AI |
| `video` | Create, list, get, delete, and download videos |
| `avatar` | List and manage avatars and looks |
| `voice` | List voices, design voices, generate speech |
| `video-translate` | Translate videos into other languages |
| `overdub` | Dub or replace audio on existing videos |
| `webhook` | Manage webhook endpoints and events |
| `asset` | Upload files for use in video creation |
| `user` | Account info and billing |

Every command supports `--help`:

```bash
heygen --help              # all command groups
heygen video --help        # video subcommands
heygen video create --help # flags, examples, and JSON schema info
```

## Complex Request Bodies

Endpoints with nested inputs (discriminated unions, arrays of objects) use `-d` for raw JSON:

```bash
# Inline JSON
heygen video-translate create -d '{
  "video": {"type": "url", "url": "https://..."},
  "output_languages": ["es"]
}'

# From a file
heygen video create -d request.json

# From stdin
cat request.json | heygen video create -d -
```

Flags and `-d` can be combined — flags override fields in the JSON.

Use `--request-schema` to discover the expected JSON shape (no auth required):

```bash
heygen video create --request-schema
heygen video-agent create --request-schema
```

## Output

**JSON by default** (for scripts and agents):

```bash
heygen video list --limit 3
# stdout: JSON array of video objects
```

**Human-readable tables** with `--human`:

```bash
heygen video list --limit 3 --human
# ID                                Title                     Status     Created
# 4621f8ba1a8f4811b32f669c37be53a2  HeyGen in 20 Seconds      completed  2026-03-28 15:48
# 75c58ba041394ddcb3737d7eff9d514b  Video Agent Weekly Recap  completed  2026-03-25 22:18
```

Make it persistent: `heygen config set output human`

**Errors** go to stderr as structured JSON:

```json
{"error": {"code": "not_found", "message": "Video not found", "hint": "Check ID with: heygen video list"}}
```

## Async Operations

Video creation is asynchronous. Two patterns:

**Block until done** (recommended):

```bash
heygen video-agent create --prompt "Demo video" --wait
# Polls until complete, then outputs the finished resource JSON
```

**Manual polling:**

```bash
heygen video create -d '...'      # returns JSON with video_id
heygen video get <video-id>       # check status
heygen video download <video-id>  # download when complete
```

`--wait` uses exponential backoff and respects `--timeout` (default 10m). Exit code 4 on timeout — stdout contains partial resource data with a hint for manual follow-up.

## Agent and CI/CD Usage

The CLI is designed for non-interactive use:

- **JSON to stdout** — always, no flags needed
- **No prompts** — all input via flags, `-d`, or stdin
- **Structured errors** — JSON envelope on stderr with `code`, `message`, `hint`
- **Exit codes** — `0` success, `1` error, `2` bad usage, `3` auth failure, `4` timeout
- **Automatic retries** — 429 and 5xx retried with backoff (respects `Retry-After`)

Set `HEYGEN_API_KEY` as an env var and the CLI is fully non-interactive.

## Self-Update

```bash
heygen update          # install latest version
heygen update check    # check without installing
```

## Configuration

| Item | Path |
|------|------|
| Credentials | `~/.heygen/credentials` |
| Config | `~/.heygen/config.toml` |

```bash
heygen config set output human   # persist --human as default
heygen config list               # show all settings with sources
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, build instructions, and how to add new commands.

## Links

- [API Documentation](https://developers.heygen.com)
- [Releases](https://github.com/heygen-com/heygen-cli/releases)
