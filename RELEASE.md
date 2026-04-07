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

1. Make sure `main` is ready for a stable cut.
2. Trigger the GitHub Actions workflow:

```bash
gh workflow run release-stable.yml -f version=v0.1.0
```

3. The workflow validates the version, creates the tag on `main`, and
   publishes the stable release artifacts via GoReleaser, then uploads the
   installer, checksums, and platform archives to S3 for CDN-backed installs.

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
