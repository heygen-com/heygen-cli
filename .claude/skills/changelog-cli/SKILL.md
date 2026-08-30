---
name: changelog-cli
description: |
  Generate a user-facing changelog for the next stable release. Reads git log
  between the last stable tag and HEAD, categorizes changes, and outputs
  formatted release notes. Use before triggering a stable release.
argument-hint: "[version, e.g. v0.0.6]"
---

# Changelog Generator

Generate release notes for a stable release by summarizing git commits since
the last stable tag.

## Step 1: Determine the range

```bash
# Find the last stable tag
LAST_STABLE=$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1)
```

If `LAST_STABLE` is empty (first stable release), use the full history by
omitting the range:

```bash
# If LAST_STABLE is set:
git log "${LAST_STABLE}..origin/main" --oneline

# If LAST_STABLE is empty (first release):
git log origin/main --oneline
```

If a version argument was provided (e.g., `/changelog-cli v0.0.6`), use it as
the heading. Otherwise, infer the next version by bumping the patch of
`$LAST_STABLE` (or use `v0.1.0` if no stable tag exists).

## Step 2: Read the full commit details

Use the same range logic from Step 1 (with or without `$LAST_STABLE`):

```bash
# With a previous stable tag:
git log "${LAST_STABLE}..origin/main" --format="### %h%n%s%n%n%b%n---"

# First release (no stable tag):
git log origin/main --format="### %h%n%s%n%n%b%n---"
```

Also read the PR descriptions for any merged PRs in the range to get richer
context on what changed and why:

```bash
# Extract PR numbers from commit messages (works on both GNU and BSD grep)
git log ${LAST_STABLE:+"${LAST_STABLE}.."}origin/main --oneline | grep -o '#[0-9]\+' | tr -d '#' | sort -u
```

For each PR number, read the PR body:

```bash
gh pr view <number> --json title,body --jq '.title + "\n" + .body'
```

## Step 3: Categorize and write the changelog

Group changes into these categories (omit empty categories):

- **New** - new commands, features, or capabilities
- **Improved** - enhancements to existing behavior
- **Fixed** - bug fixes
- **Internal** - changes with no user-visible effect (CI, refactors, docs)

A codegen resync is not automatically Internal. What it did to the command
surface is what a user cares about, so a resync that added a command or a flag
is a **New** entry naming that command or flag, carrying the resync's PR number.
List a resync under Internal only when it changed nothing a user can type.

The commit subject does not tell you which case you are in -- every resync reads
`codegen: resync gen/ from EF <sha>`. Get the surface change from the resync PR's
body, which lists new and changed commands, or from
`scripts/release-surface.sh diff <last-stable> origin/main`.

## Step 4: Format the output

Output the changelog in this format:

```markdown
## <version> (<date>)

### New
- **Short title.** One-sentence description of what changed and why it matters. (#PR)

### Improved
- **Short title.** One-sentence description. (#PR)

### Fixed
- **Short title.** One-sentence description. (#PR)

### Internal
- Updated CI workflow (#PR)
```

Rules:
- Write for CLI users, not contributors. Focus on what changed from the user's
  perspective, not implementation details.
- Each entry should be one sentence. Lead with what the user can now do or what
  was fixed, not what code changed.
- Include the PR number in parentheses at the end of each entry.
- Do not fabricate changes. Only include what is in the git log.
- Collapse CI changes and doc updates into the Internal section with minimal detail.
- Never let an Internal entry restate something already listed above it. If a
  resync's only content is the command it added, that command's New entry is the
  whole story and a second Internal line for the same PR adds nothing.

## Step 5: Present for review

Print the formatted changelog and ask the releaser to confirm or edit before
proceeding. Once confirmed, the releaser should paste it into the GitHub release
notes when the release is published.
