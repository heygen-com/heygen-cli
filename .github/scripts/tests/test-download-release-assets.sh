# shellcheck shell=bash

DL_TAG="v1.2.3"
DL_LINUX="heygen_1.2.3_linux_amd64.tar.gz"
DL_WINDOWS="heygen_1.2.3_windows_amd64.zip"

# release_fixture [checksums-extra-line]: registers release 5 for DL_TAG with
# checksums.txt (asset 10), two archives (11, 12) and an unlisted archive (13).
# Sets DL_DIGEST to the sha256 of that checksums.txt.
release_fixture() {
  local assets="$WORK/assets"
  mkdir -p "$assets"
  printf 'linux build' >"$assets/$DL_LINUX"
  printf 'windows build' >"$assets/$DL_WINDOWS"
  (cd "$assets" && sha256sum "$DL_LINUX" "$DL_WINDOWS") >"$assets/checksums.txt"
  if [[ -n "${1:-}" ]]; then
    printf '%s\n' "$1" >>"$assets/checksums.txt"
  fi
  DL_DIGEST="$(sha256sum "$assets/checksums.txt" | cut -d' ' -f1)"
  release_json "$DL_TAG" false
  respond gh api -H "Accept: application/octet-stream" repos/owner/repo/releases/assets/10 <"$assets/checksums.txt"
  respond gh api -H "Accept: application/octet-stream" repos/owner/repo/releases/assets/11 <"$assets/$DL_LINUX"
  respond gh api -H "Accept: application/octet-stream" repos/owner/repo/releases/assets/12 <"$assets/$DL_WINDOWS"
}

# release_json <tag-name> <draft>
release_json() {
  respond gh api repos/owner/repo/releases/5 <<EOF
{
  "tag_name": "$1",
  "draft": $2,
  "assets": [
    {"id": 10, "name": "checksums.txt"},
    {"id": 11, "name": "$DL_LINUX"},
    {"id": 12, "name": "$DL_WINDOWS"},
    {"id": 13, "name": "heygen_1.2.3_extra.tar.gz"}
  ]
}
EOF
}

test_download_rejects_bad_arguments() {
  local digest args
  digest="$(printf '%064d' 0)"
  for args in \
    "5x $DL_TAG $digest" \
    "5 v1.02.3 $digest" \
    "5 v1.2.4-dev.202611161200 $digest" \
    "5 $DL_TAG ${digest:1}"; do
    # shellcheck disable=SC2086
    run_script download-release-assets.sh $args "$WORK/dist"
    assert_status 1
    assert_output "bad release ID, tag or checksums digest"
  done
  assert_no_calls
}

test_download_fetches_exactly_the_listed_archives() {
  release_fixture
  run_script download-release-assets.sh 5 "$DL_TAG" "$DL_DIGEST" "$WORK/dist"
  assert_status 0
  assert_calls \
    "gh api repos/owner/repo/releases/5" \
    "gh api -H Accept: application/octet-stream repos/owner/repo/releases/assets/10" \
    "gh api -H Accept: application/octet-stream repos/owner/repo/releases/assets/11" \
    "gh api -H Accept: application/octet-stream repos/owner/repo/releases/assets/12"
  local files
  files="$(find "$WORK/dist" -mindepth 1 -printf '%f\n' | sort | paste -sd ' ')"
  [[ "$files" == "checksums.txt $DL_LINUX $DL_WINDOWS" ]] || fail "unexpected files in dist: $files"
}

test_download_refuses_a_different_checksums_file() {
  release_fixture
  run_script download-release-assets.sh 5 "$DL_TAG" "$(printf '%064d' 0)" "$WORK/dist"
  assert_status 1
  assert_output "is not the one this run built"
  assert_no_call_matching "assets/1[123]"
}

test_download_refuses_a_release_for_another_tag() {
  release_json v1.2.2 false
  run_script download-release-assets.sh 5 "$DL_TAG" "$(printf '%064d' 0)" "$WORK/dist"
  assert_status 1
  assert_output "is not the published release for ${DL_TAG}"
}

test_download_refuses_a_draft_release() {
  release_json "$DL_TAG" true
  run_script download-release-assets.sh 5 "$DL_TAG" "$(printf '%064d' 0)" "$WORK/dist"
  assert_status 1
  assert_output "is not the published release for ${DL_TAG}"
}

test_download_refuses_an_unexpected_file_in_checksums() {
  release_fixture "$(printf '%064d' 0)  install.sh"
  run_script download-release-assets.sh 5 "$DL_TAG" "$DL_DIGEST" "$WORK/dist"
  assert_status 1
  assert_output "unexpected file in checksums.txt: install.sh"
}

test_download_refuses_an_archive_that_does_not_match() {
  release_fixture
  respond gh api -H "Accept: application/octet-stream" repos/owner/repo/releases/assets/12 <<<'other bytes'
  run_script download-release-assets.sh 5 "$DL_TAG" "$DL_DIGEST" "$WORK/dist"
  assert_failed
  assert_output "${DL_WINDOWS}: FAILED"
}
