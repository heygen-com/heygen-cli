# HeyGen CLI

A Go CLI wrapping HeyGen's v3 API. Auto-generated from our OpenAPI spec. Single binary, JSON-first output.

## Install

### From source (contributors)

Requires Go 1.23+.

```bash
git clone git@github.com:heygen-com/heygen-cli.git
cd heygen-cli
make install
```

This installs `heygen` to your `$GOPATH/bin` (typically `~/go/bin`).

### From GitHub Releases (everyone else)

Download the binary for your platform from [Releases](https://github.com/heygen-com/heygen-cli/releases) and put it in your PATH.

GitHub Releases is the intended install path for non-contributors who just want to use the CLI. Source builds are mainly for contributors working on the repo itself.
For this internal repository, users still need GitHub read access to download release assets.

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
