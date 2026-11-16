#!/usr/bin/env bash
# Usage: check-cdn-publish.sh <tag> <checksums-file>
#
# Refuses a CDN publish that would move the stable pointer and installer back
# to an older version, or replace an already-published version's files with
# different ones. A re-run for the version already live passes, so a failed
# publish can still be retried. Reads the public CDN, so the release role needs
# no read access to the bucket. Needs CDN_BASE.
set -euo pipefail

tag="$1"
checksums="$2"

if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '::error::not a stable release tag: %q\n' "$tag"
  exit 1
fi

current="$(curl -fsS --retry 3 "${CDN_BASE}/stable" | tr -d '\r\n')"
if [[ ! "$current" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '::error::could not read the current stable version from %s: %q\n' "${CDN_BASE}/stable" "$current"
  exit 1
fi
newest="$(printf '%s\n%s\n' "${current#v}" "${tag#v}" | sort -V | tail -n 1)"
if [[ "v${newest}" != "$tag" ]]; then
  echo "::error::the CDN already serves ${current}, newer than ${tag}; refusing to move it back"
  exit 1
fi

# The bucket answers 403, not 404, for a missing object.
existing="$(mktemp)"
status="$(curl -sS --retry 3 -o "$existing" -w '%{http_code}' "${CDN_BASE}/releases/${tag}/checksums.txt")"
case "$status" in
  200)
    if ! cmp -s "$existing" "$checksums"; then
      echo "::error::${tag} is already on the CDN with different files; refusing to replace them"
      exit 1
    fi
    ;;
  403 | 404) ;;
  *)
    echo "::error::could not tell whether ${tag} is already on the CDN (HTTP ${status})"
    exit 1
    ;;
esac
echo "publishing ${tag} (CDN currently serves ${current})"
