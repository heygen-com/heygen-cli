---
name: heygen-cli
description: "Create AI videos, manage avatars, translate videos, and download results via the HeyGen API. Use when an agent needs to generate videos from text prompts, create avatar-based videos, translate existing videos, or automate video production workflows."
---

# HeyGen CLI

Official CLI for the HeyGen video generation API. 30+ commands auto-generated from the OpenAPI spec. All output is JSON by default.

- **API Docs**: https://developers.heygen.com
- **Install**: `curl -fsSL https://heygen-cli.heygen.com/install.sh | bash`
- **Auth**: Requires `HEYGEN_API_KEY` environment variable. The key must be provisioned by a user from https://app.heygen.com/settings/api

## Key Commands

```bash
# Create video from prompt (simplest path — blocks until done)
heygen video-agent create --prompt "Make a 30-second product demo" --wait

# Create avatar video with full control
heygen video create -d '{"video_inputs":[{"character":{"type":"avatar","avatar_id":"josh_lite"},"voice":{"type":"text","voice_id":"en_male","input_text":"Hello world"}}]}'

# Check video status
heygen video get <video-id>

# Download completed video
heygen video download <video-id>

# List resources
heygen video list --limit 5
heygen avatar list --limit 10
heygen voice list

# Translate a video
heygen video-translate create -d '{"video":{"type":"url","url":"https://..."},"output_languages":["es"]}'
```

## Async Operations

Video creation is asynchronous. Two patterns:

**Block until done (recommended):**
```bash
heygen video-agent create --prompt "Demo video" --wait
# stdout: final resource JSON with video_url when complete
```

**Manual polling:**
```bash
heygen video create -d '{"...}'       # stdout: JSON with video_id
heygen video get <video-id>           # stdout: JSON with status field
heygen video download <video-id>      # downloads file, stdout: JSON with path
```

## Request Bodies

Create/update commands accept JSON via `-d`/`--data`:
- Inline: `-d '{"key": "value"}'`
- From file: `-d request.json`
- From stdin: `echo '{"...}' | heygen video create -d -`

Flags override matching fields in the JSON when both are provided.

To discover the expected JSON fields, use `--request-schema`:
```bash
heygen video create --request-schema       # shows all request body fields with types and descriptions
heygen video-agent create --request-schema
```

To see what the API returns, use `--response-schema`:
```bash
heygen video get --response-schema         # shows all response fields
```

Both output valid JSON Schema. Available on all create/update commands (`--request-schema`) and all commands (`--response-schema`). No auth required.

## Output Contract

- **stdout**: JSON (always). Parseable by `jq`. This is the only output agents should consume.
- **stderr**: JSON error envelope on failure: `{"error":{"code":"...","message":"...","hint":"..."}}`
- **Pagination**: Use `--limit` and `--token` for manual pagination. Read `next_token` from the response to fetch the next page.
- Do not pass `--human`. It produces unstructured text that cannot be parsed.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (API error, network failure) |
| 2 | Usage error (invalid flags, missing arguments) |
| 3 | Authentication error |

## Notes

- `--wait` handles polling with exponential backoff (2s to 30s). Default timeout is 20 minutes.
- The CLI retries transient errors (429, 5xx) automatically.
- Video download writes to `{video-id}.mp4` by default. Override with `--output-path`.
- For the full API reference (concepts, limits, pricing), see https://developers.heygen.com
