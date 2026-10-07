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
marker="<!-- release-run: ${run_id} -->"

ids="$(gh api --paginate "repos/${GITHUB_REPOSITORY}/releases" \
  --jq ".[] | select(.tag_name == \"${tag}\" and .draft and ((.body // \"\") | contains(\"${marker}\"))) | .id")"

if [[ "$(grep -c . <<<"$ids")" != "1" ]]; then
  echo "::error::expected exactly one draft for ${tag} from run ${run_id}, found: ${ids:-none}"
  exit 1
fi
echo "$ids"
