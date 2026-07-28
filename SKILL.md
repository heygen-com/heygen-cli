---
name: heygen-cli
description: "Create AI videos, manage avatars, translate videos, and download results via the HeyGen API. Use when an agent needs to generate videos from text prompts, create avatar-based videos, translate existing videos, or automate video production workflows."
---

# HeyGen CLI

Official CLI for the HeyGen video generation API. 30+ commands auto-generated from the OpenAPI spec. All output is JSON by default.

- **API Docs**: https://developers.heygen.com
- **Install**: `curl -fsSL https://static.heygen.ai/cli/install.sh | bash`
- **Auth**: Requires `HEYGEN_API_KEY` environment variable. The key must be provisioned by a user from https://app.heygen.com/settings/api

## Key Commands

```bash
# Create video from prompt (simplest path — blocks until done)
heygen video-agent create --prompt "Make a 30-second product demo" --wait

# Create avatar video with full control
heygen video create -d '{"type":"avatar","avatar_id":"josh_lite","script":"Hello world","voice_id":"en_male"}'

# Check video status
heygen video get <video-id>

# Download completed video
heygen video download <video-id>

# Check for and install a newer release
heygen update

# List resources
heygen video list --limit 5
heygen avatar list --limit 10
heygen voice list

# Translate a video
heygen video-translate create -d '{"video":{"type":"url","url":"https://..."},"output_languages":["es"]}'
```

## Async Workflow

Video creation is asynchronous. Two patterns:

**Block until done (recommended):**
```bash
heygen video-agent create --prompt "Demo video" --wait
# stdout: final resource JSON with video_url when complete
# exit 4 on timeout — stdout has partial resource, stderr has the get command to poll manually
```

`--wait` only exists on `video create`, `video-agent create`, `video-translate create`,
and `lipsync create`. Every other async command needs manual polling.

**Manual polling:**
```bash
heygen video create -d '{"...}'       # stdout: JSON with video_id
heygen video get <video-id>           # stdout: JSON with status field
heygen video download <video-id>      # downloads file, stdout: JSON with path
```

**Stop conditions — a poll loop MUST have all three:**

1. **Stop on a terminal status.** `completed` and `failed` are both terminal. Only
   `pending` / `processing` / `running` mean keep going.
2. **Stop on a terminal error — do not retry it.** A non-zero exit is not
   "not ready yet". `not_found` / `*_not_found` (exit 1) means the id is wrong or the
   resource was deleted: it will never become ready, so stop. Same for `unauthorized`,
   `forbidden` (exit 3) and `usage_error` (exit 2). Only `network_error` and a 5xx
   `internal_error` are worth retrying, with backoff.
3. **Cap the loop.** Bound it by attempts or wall-clock, and exit non-zero when the cap
   is hit rather than looping forever.

Poll no faster than every 5-10s, and back off on repeated failures. An unbounded loop
that treats a 404 as "still processing" will run until something kills it: one has been
observed issuing ~450 requests an hour for three days after its session ended.

## Discovering API Fields

Use `--request-schema` and `--response-schema` on any command to see the full JSON Schema. No auth required.

```bash
heygen video create --request-schema
heygen video-agent create --request-schema
heygen video get --response-schema
```

## Output Contract

- **stdout**: JSON (always). This is the only output agents should consume.
- **stderr**: JSON error envelope on failure: `{"error":{"code":"...","message":"...","hint":"...","doc_url":"...","param":"..."}}` (`hint`/`doc_url`/`param`/`request_id` present when applicable)
- Do not pass `--human`. It produces unstructured text that cannot be parsed.

## Notes

- The CLI retries transient errors (429, 5xx) automatically.
- Use `heygen update` to check for and install a newer CLI release.
- Video download writes to `{video-id}.mp4` by default. Override with `--output-path`. Errors if the file already exists; use `--force` to overwrite.
- For the full API reference (concepts, limits, pricing), see https://developers.heygen.com
