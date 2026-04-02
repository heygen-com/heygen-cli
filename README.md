# HeyGen CLI

A Go CLI wrapping HeyGen's v3 API. Auto-generated from our OpenAPI spec. Single binary, JSON-first output.

## Install

> **This is a private repo.** All install methods require GitHub access (org membership or a personal access token with `repo` scope).

Pick the method that fits you:

### I just want to use the CLI (no Go needed)

Run the install script — it detects your platform, downloads the latest build, and installs to `~/.local/bin`:

```bash
# If you have the GitHub CLI (gh) installed and authenticated:
bash <(gh api repos/heygen-com/heygen-cli/contents/scripts/install.sh --jq '.content' | base64 -d)

# Or with a GitHub token:
export GITHUB_TOKEN=<your-token>
curl -fsSL -H "Authorization: token $GITHUB_TOKEN" \
  https://raw.githubusercontent.com/heygen-com/heygen-cli/main/scripts/install.sh | bash
```

Make sure `~/.local/bin` is in your PATH. To update, run the same command again.

### I want to build from source (contributors)

Requires Go 1.23+.

```bash
git clone git@github.com:heygen-com/heygen-cli.git
cd heygen-cli
make install    # installs to $GOPATH/bin/heygen
```

### I want to download manually

Go to [Releases](https://github.com/heygen-com/heygen-cli/releases), download the `Internal Dev Build` asset for your platform, extract, and add to PATH:

```bash
tar xzf heygen_darwin_arm64.tar.gz
sudo mv heygen /usr/local/bin/
```

## Setup

Authenticate with your HeyGen API key:

```bash
heygen auth login

# Or non-interactively
echo "$HEYGEN_API_KEY" | heygen auth login
```

The key is stored in `~/.heygen/credentials`. You can also use the `HEYGEN_API_KEY` environment variable (takes precedence over the stored key).

## Usage

```bash
heygen video list --limit 10
heygen video get <video-id>
heygen video create --avatar-id josh_lite --script "Hello world" --voice-id en_male
heygen avatar list
heygen voice list --type public
heygen video list --limit 10 --human
```

Every command supports `--help`:

```bash
heygen --help                          # show all groups
heygen video --help                    # show video commands
heygen video-agent sessions --help     # show nested sub-commands
heygen webhook --help                  # flattened nested help for endpoint/event commands
```

### Complex request bodies

For endpoints with complex input (nested objects, unions), use `-d` to pass raw JSON:

```bash
heygen video-translate create -d '{"video": {"type": "url", "url": "https://..."}, "output_languages": ["es"]}'

# Or from a file
heygen video-translate create -d request.json

# Or from stdin
cat request.json | heygen video-translate create -d -
```

Flags and `-d` can be combined — flags override fields in the JSON body.

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

This reads the OpenAPI spec, generates command definitions in `gen/`, and formats the output. Generated files should be committed.
