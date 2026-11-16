#!/usr/bin/env bash
# Installs a fixed zizmor release into /usr/local/bin, and refuses to unless the
# archive matches the hash below.
set -euo pipefail

ZIZMOR_VERSION="1.32.0"
# sha256 of zizmor-x86_64-unknown-linux-gnu.tar.gz, from the release's asset
# digest. Change it together with the version.
ZIZMOR_SHA256="bb110cc765eca9cd7efb2e284a956be5974c60027b0970f16397d86208a9957b"

cd "$(mktemp -d)"
curl -sSfL "https://github.com/zizmorcore/zizmor/releases/download/v${ZIZMOR_VERSION}/zizmor-x86_64-unknown-linux-gnu.tar.gz" -o zizmor.tar.gz
echo "${ZIZMOR_SHA256}  zizmor.tar.gz" | sha256sum -c -
tar xzf zizmor.tar.gz zizmor
sudo mv zizmor /usr/local/bin/
