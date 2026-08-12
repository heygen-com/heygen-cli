# Release Process

For install instructions, see [README.md](./README.md).

## Release Types

### Dev builds

- Immutable prereleases tagged `v{base}-dev.{YYYYMMDDHHmm}`
- Built from `main`, **cut on demand** — merging to `main` does not mint one
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

Every step compares against `origin/main` and the last stable tag, so start from a fetched checkout:

```bash
git fetch --tags origin
LAST_STABLE=$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1)
```

Keep `$LAST_STABLE` set for the rest of the checklist; steps 4 and 6 reuse it. It filters to release tags, so a run of `-dev.*` prereleases can't shadow the stable one.

1. **Review commits since last stable.** Check what's new and confirm nothing is half-finished:
   ```bash
   git log "$LAST_STABLE"..origin/main --oneline
   ```
2. **Check open PRs.** Decide if any should merge first (e.g. pending codegen resyncs, small fixes):
   ```bash
   gh pr list --state open
   ```
3. **Confirm CI is green on main.** All checks should pass on the latest commit.
4. **Diff the generated command surface for regressions.** See [Checking for Regressions](#checking-for-regressions) below. Required on every stable release, not just ones that look risky — a resync that breaks the CLI looks identical in `git log` to one that doesn't. It covers `gen/` only; hand-written commands in `cmd/heygen/` are reviewed the normal way, through their PRs.
5. **Run E2E smoke test.** With `HEYGEN_API_KEY` set, run `/e2e-cli-test` in Claude Code from the repo root. Confirm all phases pass (no FAIL). WARN on Phase 3 means the account lacks data for some get/detail commands and should be investigated. This builds the binary and exercises it against the live API (costs a small number of credits).
6. **Pick the version number.** Check the last stable tag and bump according to the rules below:
   - Patch (`v0.0.x`) for bug fixes, UX polish, codegen resyncs, and additive schema changes.
   - Minor (`v0.x.0`) for new command groups, significant new capabilities, or **any breaking surface change found in step 4** — a resync is only a patch when it is purely additive.
   ```bash
   echo "$LAST_STABLE"
   ```
7. **Generate changelog.** Run `/changelog-cli v0.x.y` in Claude Code. Review the output and save it for the release notes. The skill reads `git log`, so it cannot see the step 4 findings — add those to the release notes yourself, at the top, under **Breaking changes** if any was breaking and **Deprecated** otherwise.

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

1. **Verify the release was published:**
   ```bash
   gh release view "$VERSION"
   ```
2. **Publish the changelog you wrote in step 7.** GoReleaser fills the release body with a commit list, which is a floor, not the notes. Replace it:
   ```bash
   gh release edit "$VERSION" --notes-file notes.md
   ```
   Nothing does this for you, and nothing fails if you skip it — the release just keeps the commit list. Read the body back afterwards to confirm it took.
3. **Verify the install script picks up the new version** (after CDN propagation):
   ```bash
   curl -fsSL https://static.heygen.ai/cli/install.sh | bash
   heygen --version
   ```
   Run the installed binary rather than trusting the CDN pointer — that is the artifact a user actually gets.

## Checking for Regressions

`gen/` is generated from HeyGen's OpenAPI spec, which lives upstream. A resync lands as one `codegen: resync gen/ from EF <sha>` commit and `/changelog-cli` files it under Internal — so the commit log and the changelog, the two things a releaser reads, are exactly where a breaking change is invisible. Diff the generated surface instead. This covers `gen/` only; hand-written commands in `cmd/heygen/` and the hidden-endpoint list in `internal/command/hidden.go` are reviewed through their own PRs.

```bash
scripts/release-surface.sh diff "$LAST_STABLE" origin/main
```

The script reduces `gen/` at both refs to the fields that decide what a user can type, then diffs the two. It filters with an allowlist, since a field left out is invisible forever; `codegen/surface_allowlist_test.go` fails the build if codegen gains a field the allowlist misses, so the list cannot rot. Request and response schemas are compared by presence rather than content: their bodies churn on every resync, but whether a command *has* one decides whether `--request-schema` and `--response-schema` exist. gofmt's alignment padding is stripped before the comparison too, so re-padding a struct — which happens whenever a field with a longer name is added — is invisible rather than reported as every field being removed.

CI runs this automatically on any PR that touches `gen/` and posts the result as a comment, so a resync's surface change is visible at review time rather than weeks later at the release cut. It never fails the build: reading the result takes judgment CI cannot supply, and a blocking check would deadlock the codegen sync bot, which cannot add a deprecated alias itself. Running it here is still part of the checklist, not an optional double-check: the CI comment is advisory and easy to scroll past, and it only covers changes that arrived through a PR.

Empty output means the surface is unchanged. The script exits non-zero and says so if its own reduction matched nothing, because "no changes" and "the check is broken" otherwise look identical. Read the `<` lines (the old side) first — a removal is a break, an addition usually isn't.

| A `<` line showing... | Effect | Action |
|---|---|---|
| a command or flag `Name` gone | `unknown command` / `unknown flag`, exit 2 | For a command, re-register the old path in `cmd/heygen/aliases.go`. There is no flag equivalent, so call it out. |
| a stricter input: `Required` false→true, a dropped `Enum` value, narrowed `Min`/`Max`, changed `Type` | previously valid invocations now rejected before the request is sent | Breaking. Name the command, the flag, and the old vs new constraint. |
| `Args` losing an entry, or a changed `Param` | positional arity changed, or the same argument now fills a different URL slot | Breaking. Show the old and new call shape. |
| a changed `Source`, `JSONName`, `Default`, or `SendDefaultWhenOmitted` | same input, different request — routing, wire key, or whether a value is sent at all | Confirm it is intended; none of these change the help text, so nothing else will surface them. |
| `BodyEncoding` leaving `json` | `-d/--data` is no longer registered — the builder adds it only when `BodyEncoding` is exactly `json` | Breaking for anyone passing a raw body. |
| a `RequestSchema`/`ResponseSchema` `<present>` line gone | `--request-schema` / `--response-schema` no longer exist there | Breaking for agents that introspect before calling. |
| `Destructive`, `Endpoint`, or `Method` changing | `--force` and the confirmation prompt appear or disappear; or the command now calls something else | Rarely intended. Confirm before releasing. |

Additions are usually safe, with two exceptions that show up only as `>` lines: a new flag that is already `Required: true`, and a new `Args` entry. Both make every prior invocation of that command exit 2. (`Deprecated: true` appearing is not a break — the flag still works and still sends its value — but it is worth a release note.)

Finally, a flag can stop doing anything without changing shape at all, which the diff above cannot see because it excludes help text:

```bash
scripts/release-surface.sh deprecated "$LAST_STABLE" origin/main
```

Each line names a command and the flag that went quiet, e.g. `video-translate create --enable-caption`. It matches loosely on purpose; a false positive is obvious once you can see which flag it named. A real hit belongs in the release notes, because a user whose script sets that flag gets no error and no warning — just different output than they asked for.

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
