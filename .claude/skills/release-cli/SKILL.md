---
name: release-cli
description: |
  Cut a stable (or dev) release of the heygen CLI end to end: run RELEASE.md's
  pre-release checklist, land pending PRs, check the command surface, run E2E,
  pick the version, write the changelog, trigger the release workflow, publish
  the notes, and verify the installer. Stops for the user at every decision.
  Use when asked to "cut a release", "ship a new version", or "release the CLI".
argument-hint: "[stable | dev] (default: stable)"
---

# Release the heygen CLI

[RELEASE.md](../../../RELEASE.md) is the source of truth for every step below; this
skill runs it in order and adds where to stop and what tends to go wrong. When the
two disagree, RELEASE.md wins, and fix this file.

Work from a fresh fetch of `origin/main`, never from the local `main` checkout,
which may be behind. Set `LAST_STABLE` exactly as RELEASE.md's pre-release
checklist shows and keep it for the whole run.

## Stop points

Stop and ask the user before each of these, with what you found and a
recommendation. Never proceed on a guess.

1. **Changing any open PR** before the release (step 2): pushing to it (examples
   for a sync PR, a rebase onto `main`), approving it, or merging it.
2. **Running E2E** (step 5): it spends API credits and needs `HEYGEN_API_KEY`. Ask
   which key to use; do not go looking for one.
3. **The version** (step 6), whenever step 4 found anything breaking or you are
   unsure between patch and minor.
4. **Triggering the release workflow.** It tags `main` and publishes; show the
   version, the commit, and the changelog first.
5. **Replacing the published release notes** (step 9): show the final notes.
6. **Any recovery action** (step 8) that changes the repository or a release:
   deleting a tag or release, dispatching again, or re-running a job. Say which
   job failed, what RELEASE.md prescribes, and what the action will change.

Reading, checking, building, and local edits need no approval.

## Stable release

### 1. Review commits since the last stable tag

Follow RELEASE.md step 1. Flag anything that looks half-finished, and any PR that
says it must not ship before something else is deployed: read the bodies of the
merged PRs in the range for that.

### 2. Open PRs

Follow RELEASE.md step 2, then sort what you find:

- **An open codegen sync PR** (`codegen: resync gen/ from EF <sha>`, opened by the
  sync bot) usually belongs in the release. If its CI fails on
  `TestAllGeneratedCommandsHaveExamples`, the fix is examples for the new
  commands in `codegen/examples/<group>.yaml`, regenerated with
  `make generate SPEC=<spec> STRICT=1`. Use the spec from the EF commit the PR
  body names; first confirm that regenerating from it reproduces the bot's
  `gen/` byte for byte, then add the examples, so `gen/` changes only by them.
- **Merging is serial.** `main` requires branches to be up to date, so merging
  one PR puts the next one behind. Rebase it, confirm its diff is unchanged,
  push, and get an approval on the new head. Never merge with `--admin`.
- Merge with `gh pr merge <n> --squash --match-head-commit <sha>`, pinned to the
  commit that was reviewed and green.

### 3. CI on main

Follow RELEASE.md step 3, after any merges from step 2.

### 4. Command-surface regressions

Follow RELEASE.md step 4 and its [Checking for Regressions](../../../RELEASE.md#checking-for-regressions)
section, against `origin/main` after the merges. Read every `<` line with the
table there. The `deprecated` check matches loosely: a hit whose help text only
mentions a deprecated *value* (for example an enum alias) is a false positive;
say so rather than putting it in the notes.

### 5. E2E smoke test (stop point 2)

With the user's key, run `/e2e-cli-test` (see its skill). All phases must pass; a
Phase 3 WARN means the account lacks data, so check that the skipped commands'
lists really are empty before calling it fine.

### 6. Version (stop point 3 if anything is breaking)

Apply RELEASE.md step 6's rules. New command groups or significant new
capability mean a minor bump; anything breaking from step 4 is at least a minor.

### 7. Changelog

Run `/changelog-cli <version>` (see its skill). Then check every flag and command
it names against `gen/`: a flag that already existed in `LAST_STABLE` is not new.
Put step 4's breaking or deprecated findings at the top, as RELEASE.md says.
Save the notes to a file; you will publish them after the release.

### 8. Trigger (stop point 4)

Follow RELEASE.md's "Trigger the release" and wait for the run to finish. If any
job fails, read [If the release fails](../../../RELEASE.md#if-the-release-fails)
to find the prescribed recovery, then stop (stop point 6) before acting on it.
Do not delete tags or releases by hand unless that section says to.

### 9. After the release

Follow RELEASE.md's "Post-release": confirm the release is published, replace its
body with the saved notes once the user approves them (stop point 5) and read it
back, and verify the installer. Install into
a scratch directory (`INSTALL_DIR=<dir>`) rather than over the user's own
`heygen`, and run the installed binary's `--version`.

## Dev release

Follow RELEASE.md's "How to Cut a Dev Release". There is no checklist; confirm
the user wants one before triggering it. If it fails, "If the release fails"
applies, except that dev releases have no `publish-cdn` job. Give the release
link to the user; sharing it with anyone else is theirs to do.

## Report

End with: the version and tag commit, the release link, what merged first, the
surface findings, the E2E result, and anything left open.
