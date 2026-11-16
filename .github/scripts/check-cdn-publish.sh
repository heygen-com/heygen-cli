#!/usr/bin/env bash
# Usage: check-cdn-publish.sh <tag> <checksums-file>
#
# Refuses a CDN publish that would move the stable pointer and installer back
# to an older version, or replace an already-published version's files with
# different ones. A re-run for the version already live passes, so a failed
# publish can still be retried. Reads the bucket, not the CDN, which can serve
# a cached answer for up to a minute. Needs S3_CLI_PREFIX and AWS credentials
# with s3:GetObject and s3:ListBucket on that prefix.
set -euo pipefail

tag="$1"
checksums="$2"

if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf '::error::not a stable release tag: %q\n' "$tag"
  exit 1
fi

current="$(aws s3 cp "${S3_CLI_PREFIX}/stable" - | tr -d '\r\n')"
if [[ ! "$current" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  printf '::error::could not read the current stable version from %s: %q\n' "${S3_CLI_PREFIX}/stable" "$current"
  exit 1
fi
newest="$(printf '%s\n%s\n' "${current#v}" "${tag#v}" | sort -V | tail -n 1)"
if [[ "v${newest}" != "$tag" ]]; then
  echo "::error::the CDN already serves ${current}, newer than ${tag}; refusing to move it back"
  exit 1
fi

# Listed rather than fetched: the role may list only under cli/, and without
# list access a fetch of a missing object answers 403, not 404.
key="${S3_CLI_PREFIX#s3://}/releases/${tag}/checksums.txt"
found="$(aws s3api list-objects-v2 --bucket "${key%%/*}" --prefix "${key#*/}" --query "length(Contents[?Key=='${key#*/}'] || \`[]\`)" --output text)"
if [[ "$found" == "1" ]]; then
  existing="$(mktemp)"
  aws s3 cp --only-show-errors "${S3_CLI_PREFIX}/releases/${tag}/checksums.txt" "$existing"
  if ! cmp -s "$existing" "$checksums"; then
    echo "::error::${tag} is already on the CDN with different files; refusing to replace them"
    exit 1
  fi
elif [[ "$found" != "0" ]]; then
  echo "::error::could not tell whether ${tag} is already on the CDN (listing returned ${found})"
  exit 1
fi
echo "publishing ${tag} (CDN currently serves ${current})"
