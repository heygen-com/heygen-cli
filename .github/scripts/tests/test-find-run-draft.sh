# shellcheck shell=bash

DRAFT_TAG="v1.2.4-dev.202611161200"
DRAFT_MARKER="<!-- release-run: 42 -->"

test_find_run_draft_rejects_bad_arguments() {
  local args
  for args in "v01.2.3 42" "v1.2.3 4x2"; do
    # shellcheck disable=SC2086
    run_script find-run-draft.sh $args
    assert_status 1
    assert_output "bad tag or run ID"
  done
  assert_no_calls
}

test_find_run_draft_prints_the_one_match() {
  releases_are <<EOF
[
  {"id": 1, "tag_name": "$DRAFT_TAG", "draft": true, "body": "notes $DRAFT_MARKER"},
  {"id": 2, "tag_name": "$DRAFT_TAG", "draft": true, "body": "<!-- release-run: 43 -->"},
  {"id": 3, "tag_name": "$DRAFT_TAG", "draft": false, "body": "$DRAFT_MARKER"},
  {"id": 4, "tag_name": "v1.2.3", "draft": true, "body": "$DRAFT_MARKER"},
  {"id": 5, "tag_name": "$DRAFT_TAG", "draft": true, "body": null}
]
EOF
  run_script find-run-draft.sh "$DRAFT_TAG" 42
  assert_status 0
  assert_stdout 1
}

test_find_run_draft_requires_a_match() {
  releases_are <<<'[]'
  run_script find-run-draft.sh "$DRAFT_TAG" 42
  assert_status 1
  assert_output "found: none"
}

test_find_run_draft_refuses_two_matches() {
  releases_are <<EOF
[
  {"id": 1, "tag_name": "$DRAFT_TAG", "draft": true, "body": "$DRAFT_MARKER"},
  {"id": 2, "tag_name": "$DRAFT_TAG", "draft": true, "body": "$DRAFT_MARKER"}
]
EOF
  run_script find-run-draft.sh "$DRAFT_TAG" 42
  assert_status 1
  assert_output "expected exactly one draft"
}
