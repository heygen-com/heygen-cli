# Release Process

## Install the CLI

### Install script

Recommended for internal users who do not want to build from source.

If `gh` is installed and authenticated:

```bash
gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  --jq '.content' | base64 -d | bash
```

If you want to use a token directly:

```bash
curl -fsSL \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.raw" \
  https://api.github.com/repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  | bash
```

The installer downloads the latest stable release and installs `heygen` into
`~/.local/bin` by default.

Install the latest dev prerelease:

```bash
gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  --jq '.content' | base64 -d | bash -s -- --dev
```

Install a specific version:

```bash
gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  --jq '.content' | base64 -d | bash -s -- --version v0.1.0
```

### Manual download

1. Open the repo's Releases page.
2. Find the `Internal Dev Build` prerelease.
3. Download the archive for your platform.
4. Extract the archive and move `heygen` into your `PATH`.

### From source

For contributors with Go installed:

```bash
git clone git@github.com:heygen-com/heygen-cli.git
cd heygen-cli
make install
```

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
   publishes the stable release artifacts via GoReleaser.

## Version Scheme

- `v{base}-dev.{YYYYMMDD}.{shorthash}` for dev prereleases
- `v0.x.y` for pre-1.0 stable releases
- `v1.x.y` and beyond once the CLI surface is considered stable
