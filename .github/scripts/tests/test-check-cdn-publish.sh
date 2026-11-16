# shellcheck shell=bash

# stable_is <version>: the stable pointer in the bucket holds <version>.
stable_is() {
  printf '%s' "$1" | respond aws s3 cp s3://bucket/cli/stable -
}

# listing_for <tag>: the arguments check-cdn-publish.sh lists <tag>'s checksums.txt with.
listing_for() {
  printf '%s\n' aws s3api list-objects-v2 --bucket bucket --prefix "cli/releases/$1/checksums.txt" \
    --query "length(Contents[?Key=='cli/releases/$1/checksums.txt'] || \`[]\`)" --output text
}

# listed <tag> <count>
listed() {
  local -a call
  mapfile -t call < <(listing_for "$1")
  respond "${call[@]}" <<<"$2"
}

# published_checksums <tag> <content>
published_checksums() {
  printf '%s\n' "$2" | respond aws s3 cp --only-show-errors "s3://bucket/cli/releases/$1/checksums.txt"
}

local_checksums() {
  printf '%s\n' "$1" >"$WORK/checksums.txt"
}

test_check_cdn_rejects_bad_arguments() {
  local tag
  for tag in v1.02.3 v1.2.4-dev.202611161200; do
    run_script check-cdn-publish.sh "$tag" "$WORK/checksums.txt"
    assert_status 1
    assert_output "not a stable release tag"
  done
  assert_no_calls
}

test_check_cdn_refuses_an_older_version() {
  stable_is v1.10.0
  run_script check-cdn-publish.sh v1.9.0 "$WORK/checksums.txt"
  assert_status 1
  assert_output "already serves v1.10.0, newer than v1.9.0"
}

test_check_cdn_passes_a_retry_with_the_same_files() {
  stable_is v1.2.3
  listed v1.2.3 1
  published_checksums v1.2.3 "abc  heygen_1.2.3_linux_amd64.tar.gz"
  local_checksums "abc  heygen_1.2.3_linux_amd64.tar.gz"
  run_script check-cdn-publish.sh v1.2.3 "$WORK/checksums.txt"
  assert_status 0
  assert_output "publishing v1.2.3"
}

test_check_cdn_refuses_the_same_version_with_different_files() {
  stable_is v1.2.3
  listed v1.2.3 1
  published_checksums v1.2.3 "abc  heygen_1.2.3_linux_amd64.tar.gz"
  local_checksums "def  heygen_1.2.3_linux_amd64.tar.gz"
  run_script check-cdn-publish.sh v1.2.3 "$WORK/checksums.txt"
  assert_status 1
  assert_output "already on the CDN with different files"
}

test_check_cdn_passes_a_new_version() {
  stable_is v1.2.3
  listed v1.3.0 0
  local_checksums "abc  heygen_1.3.0_linux_amd64.tar.gz"
  run_script check-cdn-publish.sh v1.3.0 "$WORK/checksums.txt"
  assert_status 0
  assert_output "publishing v1.3.0 (CDN currently serves v1.2.3)"
}

test_check_cdn_refuses_an_unclear_listing() {
  stable_is v1.2.3
  listed v1.3.0 None
  run_script check-cdn-publish.sh v1.3.0 "$WORK/checksums.txt"
  assert_status 1
  assert_output "could not tell whether v1.3.0 is already on the CDN"
}

test_check_cdn_stops_when_the_listing_fails() {
  local -a call
  stable_is v1.2.3
  mapfile -t call < <(listing_for v1.3.0)
  respond_fail 254 "AccessDenied" "${call[@]}"
  run_script check-cdn-publish.sh v1.3.0 "$WORK/checksums.txt"
  assert_failed
  assert_no_call_matching "^aws s3 cp --only-show-errors"
  assert_output "AccessDenied"
}
