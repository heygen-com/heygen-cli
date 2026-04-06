# HeyGen CLI

A Go CLI wrapping HeyGen's v3 API. Auto-generated from our OpenAPI spec. Single
binary, JSON-first output.

## Install

### Install script

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

This installs the latest stable release into `~/.local/bin` by default.

Install the latest dev prerelease instead:

```bash
gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  --jq '.content' | base64 -d | bash -s -- --dev
```

Install a specific version:

```bash
gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh \
  --jq '.content' | base64 -d | bash -s -- --version v0.1.0
```

Check for and apply updates after install:

```bash
heygen update check
heygen update
```

### From source (contributors)

Requires Go 1.23+.

```bash
git clone git@github.com:heygen-com/heygen-cli.git
cd heygen-cli
make install
```

This installs `heygen` to your `$GOPATH/bin` (typically `~/go/bin`).

### Manual release download

Download the binary for your platform from
[Releases](https://github.com/heygen-com/heygen-cli/releases) and put it in
your `PATH`.

For this internal repository, users still need GitHub read access to download
release assets. See [RELEASE.md](./RELEASE.md) for the maintainer release
process and release workflows.

## Setup

Authenticate with your HeyGen API key:

```bash
heygen auth login

# Or non-interactively
echo "$HEYGEN_API_KEY" | heygen auth login
```

The key is stored in `~/.heygen/credentials`. You can also use the
`HEYGEN_API_KEY` environment variable (takes precedence over the stored key).

## Usage

```bash
heygen video list --limit 10
heygen video get <video-id>
heygen video create --avatar-id josh_lite --script "Hello world" --voice-id en_male
heygen avatar list
heygen voice list --type public
heygen video list --limit 10 --human
heygen update check
```

Every command supports `--help`:

```bash
heygen --help                      # show all groups
heygen video --help                # show video commands
heygen video-agent --help          # show flattened nested task help
heygen webhook --help              # show flattened endpoint/event help
```

### Complex request bodies

For endpoints with complex input (nested objects, unions), use `-d` to pass raw
JSON:

```bash
heygen video-translate create -d '{"video": {"type": "url", "url": "https://..."}, "output_languages": ["es"]}'

# Or from a file
heygen video-translate create -d request.json

# Or from stdin
cat request.json | heygen video-translate create -d -
```

Flags and `-d` can be combined. Flags override fields in the JSON body.

## Output modes

The CLI defaults to JSON output, which is the best mode for scripts and agents:

```bash
heygen video get <video-id>
```

For human-readable tables and key/value views, add `--human`:

```bash
heygen video list --limit 10 --human
heygen webhook --help
```

## Build & Test

```bash
make build    # build to bin/heygen
make install  # install to $GOPATH/bin/heygen
make test     # run all tests (mocked, no API key needed)
make lint     # golangci-lint
make clean    # remove build artifacts
```

## Regenerate commands from OpenAPI spec

```bash
make generate SPEC=/path/to/external-api.json
```

This reads the OpenAPI spec, generates command definitions in `gen/`, and
formats the output. Generated files should be committed.
