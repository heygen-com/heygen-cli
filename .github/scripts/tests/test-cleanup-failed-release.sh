# shellcheck shell=bash

CLEANUP_TAG="v1.2.4-dev.202611161200"
CLEANUP_COMMIT="1111111111111111111111111111111111111111"
CLEANUP_TAG_OBJECT="2222222222222222222222222222222222222222"
CLEANUP_MARKER="<!-- release-run: 42 -->"

# tag_ref_is <object-type>: the tag ref lookup answers with that object type.
tag_ref_is() {
  respond gh api "repos/owner/repo/git/ref/tags/${CLEANUP_TAG}" <<EOF
{"object": {"type": "$1", "sha": "$CLEANUP_TAG_OBJECT"}}
EOF
}

# tag_object_is <target-commit> <message>
tag_object_is() {
  jq -n --arg sha "$1" --arg message "$2" '{object: {sha: $sha}, message: $message}' |
    respond gh api "repos/owner/repo/git/tags/${CLEANUP_TAG_OBJECT}"
}

tag_from_this_run() {
  tag_ref_is tag
  tag_object_is "$CLEANUP_COMMIT" "Release ${CLEANUP_TAG} by GitHub Actions run 42
"
}

test_cleanup_rejects_bad_arguments() {
  local args
  for args in \
    "v1.02.3 $CLEANUP_COMMIT 42" \
    "$CLEANUP_TAG ${CLEANUP_COMMIT:1} 42" \
    "$CLEANUP_TAG $CLEANUP_COMMIT 4x2"; do
    # shellcheck disable=SC2086
    run_script cleanup-failed-release.sh $args
    assert_status 1
    assert_output "::error::"
  done
  assert_no_calls
}

test_cleanup_refuses_a_lightweight_tag() {
  tag_ref_is commit
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 1
  assert_output "is not an annotated tag"
}

test_cleanup_refuses_a_tag_from_another_run() {
  tag_ref_is tag
  tag_object_is "$CLEANUP_COMMIT" "Release ${CLEANUP_TAG} by GitHub Actions run 43"
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 1
  assert_output "was not created by run 42"
}

test_cleanup_refuses_a_tag_on_another_commit() {
  tag_ref_is tag
  tag_object_is "3333333333333333333333333333333333333333" "Release ${CLEANUP_TAG} by GitHub Actions run 42"
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 1
  assert_output "was not created by run 42"
}

test_cleanup_refuses_a_published_release() {
  tag_from_this_run
  releases_are <<EOF
[
  {"id": 7, "tag_name": "$CLEANUP_TAG", "draft": true, "body": "$CLEANUP_MARKER"},
  {"id": 8, "tag_name": "$CLEANUP_TAG", "draft": false, "body": "$CLEANUP_MARKER"}
]
EOF
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 1
  assert_output "already has a published release"
}

test_cleanup_refuses_another_runs_draft() {
  tag_from_this_run
  releases_are <<EOF
[
  {"id": 7, "tag_name": "$CLEANUP_TAG", "draft": true, "body": "$CLEANUP_MARKER"},
  {"id": 8, "tag_name": "$CLEANUP_TAG", "draft": true, "body": "<!-- release-run: 43 -->"}
]
EOF
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 1
  assert_output "draft release 8 for ${CLEANUP_TAG} is not from run 42"
}

test_cleanup_deletes_this_runs_drafts_then_the_tag() {
  tag_from_this_run
  releases_are <<EOF
[
  {"id": 7, "tag_name": "$CLEANUP_TAG", "draft": true, "body": "$CLEANUP_MARKER"},
  {"id": 8, "tag_name": "$CLEANUP_TAG", "draft": true, "body": "notes $CLEANUP_MARKER"},
  {"id": 9, "tag_name": "v1.2.3", "draft": false, "body": ""}
]
EOF
  respond gh api -X DELETE repos/owner/repo/releases/7 </dev/null
  respond gh api -X DELETE repos/owner/repo/releases/8 </dev/null
  respond git push "--force-with-lease=refs/tags/${CLEANUP_TAG}:${CLEANUP_TAG_OBJECT}" origin ":refs/tags/${CLEANUP_TAG}" </dev/null
  run_script cleanup-failed-release.sh "$CLEANUP_TAG" "$CLEANUP_COMMIT" 42
  assert_status 0
  assert_calls \
    "gh api repos/owner/repo/git/ref/tags/${CLEANUP_TAG}" \
    "gh api repos/owner/repo/git/tags/${CLEANUP_TAG_OBJECT}" \
    "gh api repos/owner/repo/git/tags/${CLEANUP_TAG_OBJECT}" \
    "gh api --paginate repos/owner/repo/releases" \
    "gh api -X DELETE repos/owner/repo/releases/7" \
    "gh api -X DELETE repos/owner/repo/releases/8" \
    "git push --force-with-lease=refs/tags/${CLEANUP_TAG}:${CLEANUP_TAG_OBJECT} origin :refs/tags/${CLEANUP_TAG}"
}
