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

`--wait` exists only on some create commands. Check rather than assume: append `--help`
to the exact command you intend to run, including any nested segments (e.g.
`heygen asset direct-uploads create --help`), and use `--wait` only if that output
lists it. Anything else needs manual polling.

**Manual polling:**
```bash
heygen video create -d '{"...}'       # stdout: JSON with video_id
heygen video get <video-id>           # stdout: JSON with status field
heygen video download <video-id>      # downloads file, stdout: JSON with path
```

**Stop conditions — a poll loop MUST have all three:**

1. **Poll the resource the create actually returned, and read its schema.** The id in
   a create response may belong to another group, so that group's own `get` is not
   always the status command — a template render is polled with `video get`, not
   `template get`. Take the id from the create response, find the `get` that reads
   that resource, and run that exact command with `--response-schema` to see the
   `status` field's possible values.

   Do not assume the vocabulary. State names differ between resources and so do
   spellings (some use `complete`, others `completed`). Note also that the schema
   lists the values but usually does not say which ones mean "still working": a state
   that is really waiting on user action is enumerated exactly like an in-progress
   one. So continue polling only on a value you can positively confirm means
   in-progress, and treat everything else as terminal — **including values you do not
   recognize and values whose meaning is ambiguous**. Stop and report rather than
   spin.
2. **Stop on a terminal error — a non-zero exit is not "not ready yet".**
   `not_found` / `*_not_found` (exit 1) means the id is wrong or the resource was
   deleted; it will never become ready. Same for `unauthorized` / `forbidden`
   (exit 3) and `usage_error` (exit 2). Retry only transient ones, with backoff:
   `network_error`, `timeout` (exit 4), `rate_limit_exceeded`, `quota_exceeded`,
   `internal_error`, `unclassified_server_error`.
3. **Cap the loop.** Bound it by attempts or wall-clock and exit non-zero at the cap
   rather than looping forever.

Poll no faster than every 5-10s.

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

- The CLI automatically retries 429 and selected transient 5xx (500/502/503/504) on
  retry-eligible requests.
- Use `heygen update` to check for and install a newer CLI release.
- Video download writes to `{video-id}.mp4` by default. Override with `--output-path`. Errors if the file already exists; use `--force` to overwrite.
- For the full API reference (concepts, limits, pricing), see https://developers.heygen.com
