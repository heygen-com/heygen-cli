#!/usr/bin/env bash

set -euo pipefail

REPO="${HEYGEN_REPO:-heygen-com/heygen-cli}"
RELEASE_TAG="${HEYGEN_RELEASE_TAG:-dev}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux\n' ;;
    Darwin) printf 'darwin\n' ;;
    *)
      fail "unsupported OS: $(uname -s). Install manually from the release assets."
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) printf 'amd64\n' ;;
    arm64 | aarch64) printf 'arm64\n' ;;
    *)
      fail "unsupported architecture: $(uname -m). Install manually from the release assets."
      ;;
  esac
}

checksum_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf 'sha256sum\n'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf 'shasum -a 256\n'
    return
  fi
  fail "missing sha256 tool (need sha256sum or shasum)"
}

github_token() {
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    printf '%s\n' "$GITHUB_TOKEN"
    return
  fi
  if [[ -n "${GH_TOKEN:-}" ]]; then
    printf '%s\n' "$GH_TOKEN"
    return
  fi
  if command -v gh >/dev/null 2>&1; then
    local token
    token="$(gh auth token 2>/dev/null || true)"
    if [[ -n "$token" ]]; then
      printf '%s\n' "$token"
      return
    fi
  fi
  fail "set GITHUB_TOKEN or authenticate gh before running this installer"
}

download_with_gh() {
  local asset_name="$1"
  local checksums_name="$2"

  if ! command -v gh >/dev/null 2>&1; then
    return 1
  fi
  if ! gh auth token >/dev/null 2>&1; then
    return 1
  fi

  if ! gh release download "$RELEASE_TAG" --repo "$REPO" --pattern "$asset_name" --dir "$TMPDIR"; then
    return 1
  fi
  if ! gh release download "$RELEASE_TAG" --repo "$REPO" --pattern "$checksums_name" --dir "$TMPDIR"; then
    return 1
  fi
  if [[ ! -f "${TMPDIR}/${asset_name}" || ! -f "${TMPDIR}/${checksums_name}" ]]; then
    return 1
  fi
  return 0
}

download_with_curl() {
  local asset_name="$1"
  local checksums_name="$2"
  local token="$3"
  local base_url

  if [[ -n "${HEYGEN_RELEASE_BASE_URL:-}" ]]; then
    base_url="${HEYGEN_RELEASE_BASE_URL%/}"
  else
    base_url="https://github.com/${REPO}/releases/download/${RELEASE_TAG}"
  fi

  curl -fsSL -H "Authorization: Bearer ${token}" \
    "${base_url}/${asset_name}" -o "${TMPDIR}/${asset_name}"
  curl -fsSL -H "Authorization: Bearer ${token}" \
    "${base_url}/${checksums_name}" -o "${TMPDIR}/${checksums_name}"
}

verify_checksum() {
  local asset_name="$1"
  local checksums_name="$2"
  local checksum_tool
  local expected actual

  checksum_tool="$(checksum_cmd)"
  expected="$(grep " ${asset_name}\$" "${TMPDIR}/${checksums_name}" | awk '{print $1}')"
  if [[ -z "$expected" ]]; then
    fail "could not find checksum for ${asset_name}"
  fi

  actual="$($checksum_tool "${TMPDIR}/${asset_name}" | awk '{print $1}')"
  if [[ "$expected" != "$actual" ]]; then
    fail "checksum verification failed for ${asset_name}"
  fi
}

install_archive() {
  local archive="$1"
  local os="$2"
  local extracted_bin="$TMPDIR/heygen"

  if [[ "$os" == "darwin" || "$os" == "linux" ]]; then
    tar -C "$TMPDIR" -xzf "$archive" heygen
  else
    fail "unsupported OS for automatic install: ${os}"
  fi

  mkdir -p "$INSTALL_DIR"
  install -m 0755 "$extracted_bin" "${INSTALL_DIR}/heygen"
}

main() {
  local os arch asset_name checksums_name token version_output

  os="$(detect_os)"
  arch="$(detect_arch)"
  asset_name="heygen_${os}_${arch}.tar.gz"
  checksums_name="checksums.txt"

  log "Detected: ${os} ${arch}"
  log "Installing heygen from ${REPO} release tag '${RELEASE_TAG}'"

  if download_with_gh "$asset_name" "$checksums_name"; then
    log "Downloaded release assets with gh"
  else
    token="$(github_token)"
    download_with_curl "$asset_name" "$checksums_name" "$token"
    log "Downloaded release assets with curl"
  fi

  verify_checksum "$asset_name" "$checksums_name"
  install_archive "${TMPDIR}/${asset_name}" "$os"

  version_output="$("${INSTALL_DIR}/heygen" --version 2>/dev/null || true)"
  if [[ -z "$version_output" ]]; then
    fail "installed binary but version check failed"
  fi

  log "Installed: ${INSTALL_DIR}/heygen"
  log "${version_output}"

  case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      log ""
      log "Add ${INSTALL_DIR} to your PATH if it is not already included."
      ;;
  esac
}

main "$@"
