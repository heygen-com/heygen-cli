#!/usr/bin/env bash
# Installs a fixed actionlint release into /usr/local/bin, and refuses to unless
# the archive matches the hash below.
set -euo pipefail

ACTIONLINT_VERSION="1.7.12"
# sha256 of actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz, from the
# release's checksums.txt. Change it together with the version.
ACTIONLINT_SHA256="8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8"

cd "$(mktemp -d)"
curl -sSfL "https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz" -o actionlint.tar.gz
echo "${ACTIONLINT_SHA256}  actionlint.tar.gz" | sha256sum -c -
tar xzf actionlint.tar.gz actionlint
sudo mv actionlint /usr/local/bin/
