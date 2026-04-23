---
name: changelog
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

# Show commits since that tag
git log "${LAST_STABLE}..origin/main" --oneline
```

If no stable tag exists, use the full history (`git log --oneline`).

If a version argument was provided (e.g., `/changelog v0.0.6`), use it as the
heading. Otherwise, infer the next version by bumping the patch of `$LAST_STABLE`.

## Step 2: Read the full commit details

```bash
git log "${LAST_STABLE}..origin/main" --format="### %h%n%s%n%n%b%n---"
```

Also read the PR descriptions for any merged PRs in the range to get richer
context on what changed and why:

```bash
# Extract PR numbers from commit messages
git log "${LAST_STABLE}..origin/main" --oneline | grep -oP '#\K[0-9]+' | sort -u
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
- **Internal** - codegen, CI, docs, refactors (collapsed, less detail)

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
- Codegen resync from EF abcdef1 (#PR)
- Updated CI workflow (#PR)
```

Rules:
- Write for CLI users, not contributors. Focus on what changed from the user's
  perspective, not implementation details.
- Each entry should be one sentence. Lead with what the user can now do or what
  was fixed, not what code changed.
- Include the PR number in parentheses at the end of each entry.
- Do not fabricate changes. Only include what is in the git log.
- Collapse codegen resyncs, CI changes, and doc updates into the Internal section
  with minimal detail.

## Step 5: Present for review

Print the formatted changelog and ask the releaser to confirm or edit before
proceeding. Once confirmed, the releaser should paste it into the GitHub release
notes when the release is published.
