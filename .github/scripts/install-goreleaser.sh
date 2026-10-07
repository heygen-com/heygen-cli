#!/usr/bin/env bash
# Installs a fixed GoReleaser release into /usr/local/bin, and refuses to unless
# the archive matches the hash below. goreleaser-action is not used because it
# runs the binary anyway when it cannot fetch checksums.txt.
set -euo pipefail

GORELEASER_VERSION="v2.18.2"
# sha256 of goreleaser_Linux_x86_64.tar.gz, from the release's checksums.txt.
# Change it together with the version.
GORELEASER_SHA256="0a96edc9d9bc594e4a41cc4d59467c182062910ab24d9d1f6dd7b667d32606d3"

cd "$(mktemp -d)"
curl -sSfL "https://github.com/goreleaser/goreleaser/releases/download/${GORELEASER_VERSION}/goreleaser_Linux_x86_64.tar.gz" -o goreleaser.tar.gz
echo "${GORELEASER_SHA256}  goreleaser.tar.gz" | sha256sum -c -
tar xzf goreleaser.tar.gz goreleaser
sudo mv goreleaser /usr/local/bin/
