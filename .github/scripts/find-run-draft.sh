#!/usr/bin/env bash
# Usage: find-run-draft.sh <tag> <run-id>
#
# Prints the ID of the one draft release for <tag> that GoReleaser made in run
# <run-id>, recognised by the release-run marker .goreleaser.yaml writes into
# the body. Fails unless exactly one matches. Needs GH_TOKEN and
# GITHUB_REPOSITORY.
set -euo pipefail

tag="$1"
run_id="$2"

# Both values go into a jq filter, so refuse anything but a stable or dev
# release tag and a numeric run ID.
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-dev\.[0-9]{12})?$ || ! "$run_id" =~ ^[0-9]+$ ]]; then
  printf '::error::bad tag or run ID: %q %q\n' "$tag" "$run_id"
  exit 1
fi
marker="<!-- release-run: ${run_id} -->"

ids="$(gh api --paginate "repos/${GITHUB_REPOSITORY}/releases" \
  --jq ".[] | select(.tag_name == \"${tag}\" and .draft and ((.body // \"\") | contains(\"${marker}\"))) | .id")"

if [[ "$(grep -c . <<<"$ids")" != "1" ]]; then
  echo "::error::expected exactly one draft for ${tag} from run ${run_id}, found: ${ids:-none}"
  exit 1
fi
echo "$ids"
