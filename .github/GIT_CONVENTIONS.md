# Git Conventions

## Branch Names

Format: `MM-DD-description_with_underscores`

```
03-30-add_video_create
04-02-fix_auth_keyring_fallback
```

Use Graphite (`gt create`) for stacked branches.

## Commit Messages

Format: `scope: description` (lowercase, imperative mood)

Use a scope when the change is clearly within one package. Omit the scope for cross-cutting changes.

```bash
# Single package
auth: add keyring credential storage
client: add retry with exponential backoff
output: add TUI table formatter

# Cross-cutting (multiple packages)
add --wait polling with status tracking
add pagination with --all flag

# Infrastructure
ci: add golangci-lint
docs: update CLAUDE.md
```

### Scopes

| Scope | Package / area |
|-------|---------------|
| `auth` | `internal/auth/` |
| `client` | `internal/client/` |
| `config` | `internal/config/` |
| `output` | `internal/output/` |
| `errors` | `internal/errors/` |
| `cmd` | `cmd/heygen/` |
| `codegen` | `codegen/` |
| `ci` | `.github/` |
| `docs` | Documentation files |

## PR Titles

Same format as commit messages — the PR title becomes the squash commit on main.

```
auth: add keyring credential storage
add --wait polling with status tracking
ci: add cross-platform test matrix
```

## PR Descriptions

Must follow the template (`.github/PULL_REQUEST_TEMPLATE`):
- `## Description` — 50+ characters, explain what and why
- `## Testing` — 10+ characters, describe how it was tested

CI enforces these minimums.
