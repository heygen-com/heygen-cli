# Release Process

For install instructions, see [README.md](./README.md).

## Release Types

### Dev builds

- Immutable prereleases tagged `v{base}-dev.{YYYYMMDD}.{shorthash}`
- Built from `main`
- Intended for internal users and fast feedback

### Stable releases

- Immutable tagged releases like `v0.1.0`
- Built from a tagged commit via GoReleaser
- Intended for milestone cuts and broader distribution later

## How to Cut a Dev Release

1. Make sure `main` is in a good state.
2. Trigger the GitHub Actions workflow:

```bash
gh workflow run dev-release.yml
```

3. Wait for the workflow to finish.
4. Verify a new prerelease was published for the computed dev tag.
5. Share the installer command or release link with internal users as needed.

## Automated Weekly Stable Release

A scheduled workflow (`.github/workflows/weekly-stable-release.yml`) opens a
stable **release PR** automatically, so the cadence no longer depends on someone
remembering to cut one. It does **not** publish; merging the PR does (see
[How to Cut a Stable Release](#how-to-cut-a-stable-release)). This keeps the
merge (and the `release` environment approval) as the human gate while removing
the "did anyone remember to release?" burden.

- **Trigger / schedule:** Cron every Monday at 09:00 UTC. Can also be run on
  demand via **Actions > Weekly Stable Release > Run workflow**
  (`workflow_dispatch`).
- **Skip behavior:** It finds the latest stable tag (`vX.Y.Z`) and counts
  commits on `main` since it. **No new commits → no PR** (empty weeks are
  skipped, not failed). If a release branch for the computed version already
  exists, it is left untouched so in-progress edits are never clobbered.
- **Versioning:** It **patch-bumps** the latest stable tag (e.g. `v0.0.11` →
  `v0.0.12`). Patch is the default because the weekly driver (codegen resyncs,
  fixes, additive schema) is a patch bump. Minor/major bumps (new command
  groups, breaking changes) are a **manual** release — see below.
- **What the PR contains:** a `release/vX.Y.Z` branch with a `releases/vX.Y.Z.md`
  draft changelog (commit subjects since the last tag), committed via the GitHub
  API so it is signed/verified. Refine it with `/changelog-cli vX.Y.Z` before
  merging.
- **Monotonic versions:** `release-stable.yml` refuses any version not strictly
  greater than the current latest stable tag, so the `stable` pointer never
  moves backward (e.g. a lingering weekly patch-bump PR after a manual
  minor/major release already shipped).

## How to Cut a Stable Release

Both the weekly automation and a manual cut converge on the same gate: a
`release/vX.Y.Z` PR is reviewed and **merged**, which triggers
`release-stable.yml` (test → tag → GoReleaser → S3) behind the `release`
environment approval. There are two ways to get that PR.

### From the weekly release PR (normal path)

1. The Monday automation opens a `release: vX.Y.Z` PR (`gh pr list --state open`).
2. **Refine the changelog.** Run `/changelog-cli vX.Y.Z` in Claude Code and
   update `releases/vX.Y.Z.md` on the release branch with the result.
3. **Confirm `main` CI is green.** The release PR only adds the notes file and
   is opened by the automation's `GITHUB_TOKEN`, so PR CI does not run on it; the
   build and `make test` run in `release-stable.yml` before publishing. If branch
   protection requires PR status checks, merge with admin (or provision a PAT/App
   token so release PRs trigger CI — tracked as a PRINFRA-170 follow-up).
4. **Run the E2E smoke test.** With `HEYGEN_API_KEY` set, run `/e2e-cli-test` in
   Claude Code from the repo root. Confirm all phases pass (no FAIL). WARN on
   Phase 3 means the account lacks data for some get/detail commands and should
   be investigated. This builds the binary and exercises it against the live API
   (costs a small number of credits).
5. **Merge the PR**, then **approve the `release` environment** when prompted.
   That publishes the release. To skip a week, just close the PR.

### Manual / off-schedule (including minor/major bumps)

1. **Pick the version** (`gh release list --limit 3`):
   - Patch (`v0.0.x`) for bug fixes, UX polish, codegen resyncs, additive schema.
   - Minor (`v0.x.0`) for new command groups or significant new capabilities.
2. Cut it one of two ways (both still gated by the `release` environment approval):
   - **Dispatch directly** (works for any version, including minor/major):
     ```bash
     gh workflow run release-stable.yml -f version=v0.1.0
     ```
     Uses `releases/v0.1.0.md` if present, otherwise GoReleaser autogenerates notes.
   - **Open a release PR by hand** (if you want the changelog reviewed first):
     create a `release/v0.1.0` branch, add `releases/v0.1.0.md` (run
     `/changelog-cli v0.1.0`), open the PR, then merge. Note the **weekly
     workflow only patch-bumps**, so don't use it for a minor/major, create the
     `release/*` branch yourself or dispatch directly.

### Post-release

`release-stable.yml` validates the version, creates the tag on `main`, publishes
the artifacts via GoReleaser, and uploads the installer, checksums, and platform
archives to S3 for CDN-backed installs. CDN propagation takes up to 1 minute for
the version pointer and 5 minutes for the install script. Then:

1. **Verify the release was published:**
   ```bash
   gh release view v0.1.0
   ```
2. **Verify the install script picks up the new version** (after CDN propagation):
   ```bash
   curl -fsSL https://static.heygen.ai/cli/install.sh | bash
   heygen --version
   ```

## Version Scheme

All versions use semver with a `v` prefix. The `v` prefix is required everywhere: git tags, `--version` output, `heygen update --version` input, JSON responses, and install script flags.

### Format

| Build type | Tag format | Example |
|---|---|---|
| Stable | `v{major}.{minor}.{patch}` | `v0.1.0` |
| Dev | `v{base}-dev.{YYYYMMDDHHmm}` | `v0.1.1-dev.202604071502` |
| Local (no ldflags) | — | `dev` |

### Ordering

Semver ordering is guaranteed:

```
v0.2.0 > v0.1.1-dev.202604071502 > v0.1.1-dev.202604071400 > v0.1.0
```

- Stable always beats prerelease of the same base version
- Dev builds sort chronologically by minute-precision timestamp
- Dev builds of the next version sort above the current stable

### Dev version auto-derivation

The dev release workflow auto-computes the version tag. No manual bumping or VERSION file:

1. Reads the latest stable tag (e.g., `v0.1.0`)
2. Bumps patch: `v0.1.0` → `v0.1.1`
3. Appends `-dev.YYYYMMDDHHmm`: `v0.1.1-dev.202604071502`

If no stable tag exists, starts from `v0.0.1-dev.*`.

### Bumping rules

- **Pre-1.0 (`v0.x.y`):** no stability guarantees. Minor = features, patch = fixes.
- **Post-1.0 (`v1.0.0`+):** semver contract. Major = breaking changes to output format or flag behavior.

### Update channels

`heygen update` auto-detects the update channel from the current version:

- Running a stable version (e.g., `v0.1.0`) → updates track stable releases only
- Running a dev version (e.g., `v0.1.1-dev.*`) → updates track dev prereleases

`heygen update --version v0.1.0` overrides channel detection and installs the exact version specified.
