---
name: e2e-cli-test
description: |
  End-to-end test of the heygen CLI against the live API. Builds the binary,
  then exercises auth, list, get, --human, schema, error handling, and the
  full create-poll-download-delete write path. Spends a small number of API
  credits per run. Use before cutting a stable release.
---

# E2E CLI Test

Pre-release validation that exercises `./bin/heygen` against the live HeyGen API.

## Prerequisites

- `HEYGEN_API_KEY` must be set in the environment
- Working directory must be the heygen-cli repo root

## Workflow

Run each phase in order. Report results as you go. If a phase fails, continue
to the next phase (do not abort early) so the final report covers everything.
Always use `./bin/heygen` (the freshly built binary), never a globally installed `heygen`.

### Step 1: Build

```bash
make build
```

If this fails, stop and report the build error. Nothing else can run.

### Step 2: Phase 1 -- Auth and account

```bash
./bin/heygen auth status
./bin/heygen user me get
```

- Assert `auth status` exits 0
- Assert `user me get` exits 0, stdout is valid JSON, and `jq -e '.data.username'` succeeds
- If either command fails, stop and report the auth error. Every subsequent phase requires a valid key.

### Step 3: Phase 2 -- Read-only list commands

Run each command below. Assert exit 0 and valid JSON stdout for every one.
Save the JSON output from each -- Phase 3 will extract IDs from these results.

```bash
./bin/heygen video list --limit 1
./bin/heygen avatar list --limit 1
./bin/heygen avatar looks list --limit 1
./bin/heygen voice list --limit 1
./bin/heygen video-translate list --limit 1
./bin/heygen video-translate languages list
./bin/heygen video-agent list --limit 1
./bin/heygen video-agent styles list --limit 1
./bin/heygen lipsync list --limit 1
./bin/heygen webhook endpoints list
./bin/heygen webhook event-types list
./bin/heygen webhook events list
```

### Step 4: Phase 3 -- Read-only get/detail commands

For each command below, extract the required ID from the corresponding Phase 2
list result. If a list returned an empty `.data` array, skip that detail command
and mark it as SKIPPED (not FAIL). If more than half of the detail commands are
skipped, mark the phase as WARN and print a warning that the account lacks
sufficient data for meaningful get/detail coverage.

```bash
./bin/heygen video get <video-id>              # .data[0].id from video list
./bin/heygen avatar get <group-id>             # .data[0].id from avatar list
./bin/heygen avatar looks get <look-id>        # .data[0].id from avatar looks list
./bin/heygen video-agent get <session-id>      # .data[0].session_id from video-agent list
./bin/heygen video-agent videos list <session-id>
./bin/heygen video-translate get <id>          # .data[0].id from video-translate list
./bin/heygen lipsync get <id>                  # .data[0].id from lipsync list
./bin/heygen voice get <voice-id>              # .data[0].voice_id from voice list
```

For `video-agent resources get`: first run `./bin/heygen video-agent get <session-id>`,
then look for a `resource_id` in `messages[*].resource_ids[*]`. Only run
`./bin/heygen video-agent resources get <session-id> <resource-id>` if both values
are available; otherwise skip.

Assert exit 0 and valid JSON for each command that runs.

### Step 5: Phase 4 -- --human output mode

```bash
./bin/heygen video list --limit 1 --human
./bin/heygen avatar list --limit 1 --human
./bin/heygen voice list --limit 1 --human
```

Assert exit 0. Assert stdout is NOT valid JSON (it should be a formatted table).

### Step 6: Phase 5 -- Schema introspection

These do not make API calls.

```bash
./bin/heygen video create --request-schema
./bin/heygen video create --response-schema
```

Assert exit 0 and stdout is valid JSON for each.

### Step 7: Phase 6 -- Error handling

```bash
# Invalid API key -- expect exit 3 (auth error)
HEYGEN_API_KEY=sk_invalid_key_000 ./bin/heygen user me get

# Missing required flags -- expect exit 2 (usage error)
./bin/heygen video get
```

Assert the expected exit codes. Temporarily override `HEYGEN_API_KEY` for the
auth test only; restore the real key afterward.

### Step 8: Phase 7 -- Write path (costs credits)

This phase must always clean up, even on failure.

```bash
# Create a short video
./bin/heygen video-agent create --prompt "Say hello world" --wait
```

- Accept exit 0 (completed) or exit 4 (poll timeout) as success
- Extract `video_id` from stdout (present in both cases)
- If exit code is anything other than 0 or 4, or `video_id` is missing, fail immediately but still run cleanup below

```bash
# Verify the video exists
./bin/heygen video get <video_id>
```

- If create returned exit 4, continue polling with `./bin/heygen video get <video_id>`
  in a bounded loop (e.g., every 15 seconds for up to 5 minutes) until `.data.status`
  reaches `completed` or `failed`
- If status reaches `failed` or never reaches `completed`, mark the phase as FAIL

```bash
# Download the completed video (use a unique temp path to avoid collisions)
DOWNLOAD_PATH="/tmp/e2e-cli-test-$$-$(date +%s).mp4"
./bin/heygen video download <video_id> --output-path "$DOWNLOAD_PATH" --force
```

- Assert exit 0, stdout is valid JSON with a `.path` field
- Assert the file exists at `$DOWNLOAD_PATH` with non-zero size

```bash
# Cleanup (unconditional -- always runs if video_id was extracted)
./bin/heygen video delete <video_id> --force
rm -f "$DOWNLOAD_PATH"
```

### Step 9: Report

Print a summary table:

```
Phase                    Result
-----                    ------
1. Auth and account      PASS
2. List commands         PASS (12/12)
3. Get/detail commands   PASS (6/7, 1 skipped)   # or WARN if >50% skipped
4. --human output        PASS
5. Schema introspection  PASS
6. Error handling        PASS
7. Write path            PASS
```

If any phase is FAIL, end with a clear message identifying which phase(s) failed
and the first failing command with its exit code and stderr.
