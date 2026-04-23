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

## How to Cut a Stable Release

### Pre-release checklist

1. **Review commits since last stable.** Check what's new and confirm nothing is half-finished:
   ```bash
   git log $(gh release view --json tagName -q .tagName)..origin/main --oneline
   ```
2. **Check open PRs.** Decide if any should merge first (e.g. pending codegen resyncs, small fixes):
   ```bash
   gh pr list --state open
   ```
3. **Confirm CI is green on main.** All checks should pass on the latest commit.
4. **Run E2E smoke test.** With `HEYGEN_API_KEY` set, run `/e2e-cli-test` in Claude Code from the repo root. Confirm all phases pass (no FAIL). WARN on Phase 3 means the account lacks data for some get/detail commands and should be investigated. This builds the binary and exercises it against the live API (costs a small number of credits).
5. **Pick the version number.** Check the last stable tag and bump according to the rules below:
   - Patch (`v0.0.x`) for bug fixes, UX polish, codegen resyncs, and additive schema changes.
   - Minor (`v0.x.0`) for new command groups or significant new capabilities.
   ```bash
   gh release list --limit 3
   ```

### Trigger the release

From the CLI:

```bash
gh workflow run release-stable.yml -f version=v0.0.5
```

Or from the GitHub Actions UI: go to **Actions > Stable Release > Run workflow**, enter the version tag, and click **Run workflow**.

### Post-release

The workflow validates the version, creates the tag on `main`, and
publishes the stable release artifacts via GoReleaser, then uploads the
installer, checksums, and platform archives to S3 for CDN-backed installs.
CDN propagation takes up to 1 minute for the version pointer and 5 minutes
for the install script.

5. **Verify the release was published:**
   ```bash
   gh release view v0.0.5
   ```
6. **Verify the install script picks up the new version** (after CDN propagation):
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
