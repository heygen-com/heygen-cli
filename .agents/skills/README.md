# Maintainer skills

Skills for working on this repository, in the open [Agent Skills](https://agentskills.io)
format, so any agent that supports it can use them. They live here in
`.agents/skills/`, where Codex and other agents look; `.claude/skills/` holds
symlinks to them for Claude Code. Run one by name: `/<name>` in Claude Code,
`$<name>` (or `/skills`) in Codex.

Keep their frontmatter to the standard's fields (`name`, `description`, and the
optional `license`, `compatibility`, `metadata`, `allowed-tools`); agent-specific
fields break other agents. A new skill needs its folder here and a symlink in
`.claude/skills/`.

These are for maintainers developing and releasing the CLI. They are not the
skills for agents that *use* the CLI: that is the root [SKILL.md](../../SKILL.md),
and README's "Agent skills" section links HeyGen's public skills collection.

| Skill | What it does | When |
|---|---|---|
| [`release-cli`](release-cli/SKILL.md) | Runs [RELEASE.md](../../RELEASE.md)'s whole release, stopping for a person at each decision. Uses the two below. | Cutting a stable or dev release |
| [`e2e-cli-test`](e2e-cli-test/SKILL.md) | Builds the binary and exercises it against the live API. Spends a few credits. | Release step 5, or after a change you want to see work for real |
| [`changelog-cli`](changelog-cli/SKILL.md) | Drafts release notes from the commits since the last stable tag. | Release step 7 |

When you add a list or get command, also add it to `e2e-cli-test` (see AGENTS.md,
"New list commands").
