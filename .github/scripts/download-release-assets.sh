#!/usr/bin/env bash
# Usage: download-release-assets.sh <release-id> <tag> <checksums-sha256> <dir>
#
# Downloads the published release <release-id> into <dir>: its checksums.txt,
# which must hash to <checksums-sha256>, then exactly the archives that file
# lists, each checked against it. The release job records <checksums-sha256>
# from GoReleaser's own output, so an asset replaced on the release afterwards
# fails here. Needs GH_TOKEN and GITHUB_REPOSITORY.
set -euo pipefail

release_id="$1"
tag="$2"
checksums_sha256="$3"
dir="$4"
repo="$GITHUB_REPOSITORY"

if [[ ! "$release_id" =~ ^[0-9]+$ || ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ || ! "$checksums_sha256" =~ ^[0-9a-f]{64}$ ]]; then
  printf '::error::bad release ID, tag or checksums digest: %q %q %q\n' "$release_id" "$tag" "$checksums_sha256"
  exit 1
fi

release="$(gh api "repos/${repo}/releases/${release_id}")"
if [[ "$(jq -r '.tag_name' <<<"$release")" != "$tag" || "$(jq -r '.draft' <<<"$release")" != "false" ]]; then
  echo "::error::release ${release_id} is not the published release for ${tag}"
  exit 1
fi

download() {
  local name="$1" id
  id="$(jq -r --arg name "$name" '[.assets[] | select(.name == $name) | .id] | if length == 1 then .[0] else "" end' <<<"$release")"
  if [[ ! "$id" =~ ^[0-9]+$ ]]; then
    echo "::error::release ${release_id} has no single asset named ${name}"
    exit 1
  fi
  gh api -H "Accept: application/octet-stream" "repos/${repo}/releases/assets/${id}" >"${dir}/${name}"
}

mkdir -p "$dir"
download checksums.txt
if [[ "$(sha256sum "${dir}/checksums.txt" | cut -d' ' -f1)" != "$checksums_sha256" ]]; then
  echo "::error::checksums.txt on release ${release_id} is not the one this run built"
  exit 1
fi

while read -r _ name; do
  if [[ ! "$name" =~ ^heygen_[A-Za-z0-9._-]+\.(tar\.gz|zip)$ ]]; then
    printf '::error::unexpected file in checksums.txt: %q\n' "$name"
    exit 1
  fi
  download "$name"
done <"${dir}/checksums.txt"

(cd "$dir" && sha256sum --strict -c checksums.txt)
