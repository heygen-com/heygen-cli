#!/usr/bin/env bash

set -euo pipefail

CDN_BASE="${HEYGEN_CDN_BASE_URL:-https://static.heygen.ai/cli}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
REQUESTED_VERSION=""

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

download() {
  local url="$1"
  local output="${2:-}"

  if command -v curl >/dev/null 2>&1; then
    if [[ -n "$output" ]]; then
      curl -fsSL "$url" -o "$output"
    else
      curl -fsSL "$url"
    fi
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    if [[ -n "$output" ]]; then
      wget -q -O "$output" "$url"
    else
      wget -q -O - "$url"
    fi
    return
  fi

  fail "curl or wget required"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --version)
        if [[ $# -lt 2 ]]; then
          fail "--version requires a value"
        fi
        REQUESTED_VERSION="$2"
        if [[ ! "$REQUESTED_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
          fail "--version only supports stable releases (for example: v0.1.0)"
        fi
        shift 2
        ;;
      -h | --help)
        cat <<'EOF'
Usage: install.sh [--version <tag>]

Install the HeyGen CLI stable release for the current platform.

Options:
  --version <tag>    Install a specific version tag (for example: v0.1.0)
EOF
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done
}

latest_stable_tag() {
  local version
  if ! version="$(download "${CDN_BASE}/stable")"; then
    fail "failed to resolve latest stable version from ${CDN_BASE}/stable"
  fi
  printf '%s\n' "$version" | tr -d '\r\n'
}

resolve_release_tag() {
  if [[ -n "$REQUESTED_VERSION" ]]; then
    printf '%s\n' "$REQUESTED_VERSION"
    return
  fi
  latest_stable_tag
}

download_with_cdn() {
  local release_tag="$1"
  local asset_name="$2"
  local checksums_name="$3"
  local release_url="${CDN_BASE}/releases/${release_tag}"

  if ! download "${release_url}/${asset_name}" "${TMPDIR}/${asset_name}"; then
    fail "failed to download ${asset_name} from ${release_url}"
  fi
  if ! download "${release_url}/${checksums_name}" "${TMPDIR}/${checksums_name}"; then
    fail "failed to download ${checksums_name} from ${release_url}"
  fi
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

normalize_arch() {
  local os="$1"
  local arch="$2"

  if [[ "$os" == "darwin" && "$arch" == "amd64" ]]; then
    if [[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" == "1" ]]; then
      printf 'arm64\n'
      return
    fi
  fi

  printf '%s\n' "$arch"
}

main() {
  local os arch release_tag asset_name checksums_name version_output

  parse_args "$@"
  os="$(detect_os)"
  arch="$(normalize_arch "$os" "$(detect_arch)")"
  release_tag="$(resolve_release_tag)"
  if [[ -z "$release_tag" ]]; then
    fail "could not determine release tag"
  fi
  asset_name="heygen_${release_tag}_${os}_${arch}.tar.gz"
  checksums_name="checksums.txt"

  log "Detected: ${os} ${arch}"
  log "Installing heygen release tag '${release_tag}' from ${CDN_BASE}"

  download_with_cdn "$release_tag" "$asset_name" "$checksums_name"
  log "Downloaded release assets from CDN"

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
