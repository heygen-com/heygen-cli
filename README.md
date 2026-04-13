# HeyGen CLI

[![CI](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/heygen-com/heygen-cli/actions/workflows/ci.yml)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

**A CLI your agent can actually drive.** The official HeyGen CLI — built to be the canonical tool-call surface for Claude Code, Codex, Cursor, and any other coding agent that generates AI video, voice, or avatar content. Also great for humans.

![demo](docs/demo.gif)

Full reference: **[developers.heygen.com/cli](https://developers.heygen.com/cli)**.

## Why agents pick this CLI

- **JSON in, JSON out.** Stdout is parseable JSON by default — pipe straight into `jq` or your tool loop.
- **Schema introspection built in.** `--request-schema` returns the exact input JSON Schema on any command that takes a body; `--response-schema` returns the output shape on any command with a structured response. No auth, no API call.
- **Contractual errors.** Structured `{error: {code, message, hint}}` on stderr; stable exit codes (`0` ok, `1` API, `2` usage, `3` auth, `4` timeout with partial result on stdout).
- **Non-interactive by default.** Set `HEYGEN_API_KEY` and nothing reads a TTY. The one stdin-reading command (`auth login`) is explicitly labeled and accepts pipes.
- **Built-in retries and `--wait` for async jobs.** 429/5xx back off automatically; long jobs can block with polling and resume on timeout.

## Install

```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash
```

Installs the latest stable release into `~/.local/bin`. macOS (Apple Silicon + Intel) and Linux (x64 + arm64). Windows via WSL.

<details>
<summary>Other install methods</summary>

**Specific version:**
```bash
curl -fsSL https://static.heygen.ai/cli/install.sh | bash -s -- --version v0.1.0
```

**From source** (Go 1.25+):
```bash
git clone https://github.com/heygen-com/heygen-cli.git
cd heygen-cli && make install
```
</details>

## Authenticate

The fastest path for agents and CI is an env var — no login step, no state on disk:

```bash
export HEYGEN_API_KEY=your-key-here
```

If you prefer to persist credentials to `~/.heygen/credentials`, `auth login` reads the key from stdin — interactively or piped:

```bash
heygen auth login                            # interactive prompt (dev machine)
echo "$HEYGEN_API_KEY" | heygen auth login   # piped (script or agent)
heygen auth status                           # verify either path
```

Get a key at [app.heygen.com/settings/api](https://app.heygen.com/settings/api).

## Agent quick start

Discover any command's input shape without reading docs:

```bash
heygen video-agent create --request-schema
# → JSON Schema describing the exact request body. No auth required.
```

Invoke with a JSON body built from the schema:

```bash
echo '{"prompt": "30-second product demo", "orientation": "landscape"}' \
  | heygen video-agent create -d - --wait
```

Branch on exit code:

```bash
heygen video get $VIDEO_ID
case $? in
  0) echo "ready" ;;
  1) echo "api error — stderr has the JSON envelope" ;;
  3) echo "auth problem — rotate key" ;;
  4) echo "timed out — stdout has partial resource for resume" ;;
esac
```

Chain with other tools:

```bash
heygen video list --limit 5 \
  | jq -r '.data[] | select(.status == "completed") | .video_id' \
  | xargs -n1 heygen video download
```

## Commands

Mirrors the [HeyGen v3 API](https://developers.heygen.com). Pattern: `heygen <noun> <verb>`.

| Group | What it does |
|-------|-------------|
| `video-agent` | Create videos from text prompts using AI |
| `video` | Create, list, get, delete, and download videos |
| `avatar` | List and manage avatars and looks |
| `voice` | List voices, design voices, generate speech |
| `video-translate` | Translate videos into other languages |
| `lipsync` | Dub or replace audio on existing videos |
| `webhook` | Manage webhook endpoints and events |
| `asset` | Upload files for use in video creation |
| `user` | Account info and billing |

Every command supports `--help`. Commands with a JSON request body also expose `--request-schema`; commands with a structured response also expose `--response-schema`:

```bash
heygen --help                               # all groups
heygen video-agent create --help            # flags, examples, semantics
heygen video-agent create --request-schema  # input JSON schema (create has a body)
heygen video-agent create --response-schema # output JSON schema
heygen video list --response-schema         # list has no body, but has a response schema
```

## The machine contract

### Output

**JSON on stdout, always.** No flag required. Every command — including `video download` — emits parseable JSON on stdout; binary download content writes to disk, not to stdout.

```bash
heygen video list --limit 3
# [{"video_id": "...", "status": "completed", ...}, ...]
```

### Errors

Errors go to stderr as a structured envelope:

```json
{"error": {"code": "not_found", "message": "Video not found", "hint": "Check ID with: heygen video list"}}
```

Stable `code` values for programmatic branching. Human-readable `message` and `hint` for when the agent needs to surface the error to its user.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | API error or network failure (details in stderr envelope) |
| `2` | Usage error — invalid flags, missing required arguments |
| `3` | Authentication error — missing or invalid API key |
| `4` | Timeout under `--wait` — stdout contains partial resource for resume |

### Request bodies

Simple inputs use flags. Complex inputs (nested objects, discriminated unions, arrays) use `-d`, which accepts inline JSON, a file path, or `-` for stdin:

```bash
heygen video-translate create -d '{"video": {"type": "url", "url": "..."}, "output_languages": ["es"]}'
heygen video create -d request.json
cat request.json | heygen video create -d -
```

Flags and `-d` compose — flags override matching fields in the JSON body.

### Async operations

Video and translation jobs are long-running. Two patterns:

**`--wait`** (blocks with backoff, respects `--timeout`, default 20m):

```bash
heygen video-agent create --prompt "Demo" --wait
```

**Manual polling** (for agents that want to manage their own control flow):

```bash
JOB=$(heygen video create -d request.json | jq -r .data.video_id)
heygen video get "$JOB"               # check status
heygen video download "$JOB"          # when complete
```

On `--wait` timeout, exit code is `4` and stdout contains the partial resource — no state is lost, the agent can resume by polling.

### Retries

`429 Too Many Requests` and `5xx` responses are retried automatically with exponential backoff. `Retry-After` headers are honored. Your agent doesn't need its own retry wrapper for transient failures.

## Downloading videos

Binary content writes to disk. Stdout receives a JSON envelope with the resolved `path` so agents can chain the result:

```bash
heygen video download <video-id>                          # file → ./<video-id>.mp4
heygen video download <video-id> --output-path my.mp4     # choose path
heygen video download <video-id> --asset captioned        # captioned version
# stdout: {"asset": "video", "message": "Downloaded video to ./<id>.mp4", "path": "./<id>.mp4"}
```

## Humans welcome too

The CLI is agent-first but not agent-only. For interactive use, `--human` renders tables:

```bash
heygen video list --limit 3 --human
# ID                                Title                     Status     Created
# 4621f8ba1a8f4811b32f669c37be53a2  HeyGen in 20 Seconds      completed  2026-03-28 15:48
# 75c58ba041394ddcb3737d7eff9d514b  Video Agent Weekly Recap  completed  2026-03-25 22:18
```

Make it the default for your shell:

```bash
heygen config set output human
```

## Self-update

```bash
heygen update                    # install latest
heygen update --version v0.1.0   # install a specific version
```

## Configuration

| Item | Path |
|------|------|
| Credentials | `~/.heygen/credentials` |
| Config | `~/.heygen/config.toml` |

```bash
heygen config list               # show all settings with sources
heygen config set output human   # persist --human as default
```

`HEYGEN_API_KEY` env var overrides credentials file. `HEYGEN_OUTPUT` env var overrides config file.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup and how to add new commands.

## License

[Apache License 2.0](./LICENSE)

## Links

- [CLI Documentation](https://developers.heygen.com/cli)
- [API Documentation](https://developers.heygen.com)
- [Releases](https://github.com/heygen-com/heygen-cli/releases)
