#!/usr/bin/env bash
# Usage: cleanup-failed-release.sh <tag> <commit> <run-id>
#
# Undoes a release run that pushed <tag> but did not finish publishing, so the
# version can be dispatched again. Only drafts carrying this run's release-run
# marker (written by .goreleaser.yaml) are deleted; GoReleaser creates drafts
# and a later job publishes them, so this never races the pipeline's own
# publish. Any other release for the tag stops it.
# A published release (someone else published it) is never touched:
# the script stops for a person instead, because users may already have it.
# A missing tag means nothing to do; any other unclear answer stops it, so
# nothing is deleted on a guess. The tag is deleted only if it is the one this run made: annotated,
# its message naming <run-id>, and pointing at <commit>. Anything else stops it
# as well. Safe to re-run.
# Needs GH_TOKEN with contents: write, and GITHUB_REPOSITORY.
set -euo pipefail

tag="$1"
commit="$2"
run_id="$3"
repo="$GITHUB_REPOSITORY"

if out="$(gh api "repos/${repo}/git/ref/tags/${tag}" --jq '.object.type + " " + .object.sha' 2>&1)"; then
  read -r type sha <<<"$out"
  if [[ "$type" != "tag" ]]; then
    echo "::error::${tag} is not an annotated tag, so this run did not create it; leaving it in place"
    exit 1
  fi
  target="$(gh api "repos/${repo}/git/tags/${sha}" --jq '.object.sha')"
  message="$(gh api "repos/${repo}/git/tags/${sha}" --jq '.message | rtrimstr("\n")')"
  if [[ "$target" != "$commit" || "$message" != "Release ${tag} by GitHub Actions run ${run_id}" ]]; then
    echo "::error::${tag} was not created by run ${run_id} at ${commit}; leaving it in place"
    exit 1
  fi
  tag_object="$sha"
elif grep -q 'HTTP 404' <<<"$out" && gh api "repos/${repo}" --jq '.id' >/dev/null; then
  # A 404 means "no such tag" only once the repository itself answers; GitHub
  # also returns 404 for a repository the token cannot see. GoReleaser runs only
  # after the push, so with no tag nothing for this tag can be this run's.
  echo "tag ${tag} was never pushed; nothing to delete"
  exit 0
else
  echo "$out" >&2
  echo "::error::could not tell whether ${tag} exists (the tag lookup failed, or the repository did not answer); leaving it in place"
  exit 1
fi

# Lists every release for the tag, drafts included; the releases/tags/<tag>
# endpoint cannot see drafts. A failing call aborts the script.
marker="<!-- release-run: ${run_id} -->"
releases="$(gh api --paginate "repos/${repo}/releases" \
  --jq ".[] | select(.tag_name == \"${tag}\") | \"\(.id) \(.draft) \((.body // \"\") | contains(\"${marker}\"))\"")"

while read -r id draft ours; do
  [[ -z "$id" ]] && continue
  if [[ "$draft" != "true" ]]; then
    echo "::error::${tag} already has a published release; leaving it and the tag in place. See RELEASE.md, 'If the release fails'."
    exit 1
  fi
  if [[ "$ours" != "true" ]]; then
    echo "::error::draft release ${id} for ${tag} is not from run ${run_id}; leaving it and the tag in place"
    exit 1
  fi
done <<<"$releases"

while read -r id _; do
  [[ -z "$id" ]] && continue
  echo "deleting draft release ${id} for ${tag}"
  gh api -X DELETE "repos/${repo}/releases/${id}"
done <<<"$releases"

echo "deleting tag ${tag}"
# Leased, so it deletes the tag only if it is still the object checked above.
git push --force-with-lease="refs/tags/${tag}:${tag_object}" origin ":refs/tags/${tag}"
