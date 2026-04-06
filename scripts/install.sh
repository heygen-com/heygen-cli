#!/usr/bin/env bash

set -euo pipefail

REPO="${HEYGEN_REPO:-heygen-com/heygen-cli}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
MODE="stable"
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
  return 1
}

json_get_string() {
  local key="$1"
  sed -n "s/.*\"${key}\":\"\\([^\"]*\\)\".*/\\1/p"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dev)
        if [[ -n "$REQUESTED_VERSION" ]]; then
          fail "--dev cannot be combined with --version"
        fi
        MODE="dev"
        shift
        ;;
      --version)
        if [[ $# -lt 2 ]]; then
          fail "--version requires a value"
        fi
        if [[ "$MODE" == "dev" ]]; then
          fail "--version cannot be combined with --dev"
        fi
        REQUESTED_VERSION="$2"
        if [[ ! "$REQUESTED_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.].+)?$ ]]; then
          fail "--version must include the leading v (for example: v0.1.0)"
        fi
        MODE="version"
        shift 2
        ;;
      -h | --help)
        cat <<'EOF'
Usage: install.sh [--dev] [--version <tag>]

Install the HeyGen CLI release for the current platform.

Options:
  --dev              Install the latest dev prerelease
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

github_api() {
  local path="$1"
  local token
  local url="https://api.github.com/repos/${REPO}${path}"

  if token="$(github_token)"; then
    curl -fsSL \
      -H "Authorization: Bearer ${token}" \
      -H "Accept: application/vnd.github+json" \
      "$url"
    return
  fi

  curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "$url"
}

latest_stable_tag() {
  github_api "/releases/latest" | tr -d '\n' | json_get_string tag_name
}

latest_dev_tag() {
  if command -v gh >/dev/null 2>&1 && gh auth token >/dev/null 2>&1; then
    gh release list --repo "$REPO" --exclude-drafts --limit 50 --json tagName,isPrerelease --jq 'map(select(.isPrerelease))[0].tagName'
    return
  fi

  github_api "/releases?per_page=50" | tr -d '\n' | sed 's/[[:space:]]//g' | awk '
    BEGIN { RS="\\{"; FS="," }
    {
      tag=""; prerelease=""; draft=""
      for (i = 1; i <= NF; i++) {
        if ($i ~ /"tag_name":"/) {
          field=$i
          sub(/.*"tag_name":"/, "", field)
          sub(/".*/, "", field)
          tag=field
        }
        if ($i ~ /"prerelease":/) {
          field=$i
          sub(/.*"prerelease":/, "", field)
          sub(/[^a-z].*/, "", field)
          prerelease=field
        }
        if ($i ~ /"draft":/) {
          field=$i
          sub(/.*"draft":/, "", field)
          sub(/[^a-z].*/, "", field)
          draft=field
        }
      }
      if (tag != "" && prerelease == "true" && draft != "true") {
        print tag
        exit
      }
    }
  '
}

resolve_release_tag() {
  case "$MODE" in
    stable)
      latest_stable_tag
      ;;
    dev)
      latest_dev_tag
      ;;
    version)
      printf '%s\n' "$REQUESTED_VERSION"
      ;;
  esac
}

download_with_gh() {
  local release_tag="$1"
  local asset_name="$2"
  local checksums_name="$3"

  if ! command -v gh >/dev/null 2>&1; then
    return 1
  fi
  if ! gh auth token >/dev/null 2>&1; then
    return 1
  fi

  if ! gh release download "$release_tag" --repo "$REPO" --pattern "$asset_name" --dir "$TMPDIR" >/dev/null 2>&1; then
    return 1
  fi
  if ! gh release download "$release_tag" --repo "$REPO" --pattern "$checksums_name" --dir "$TMPDIR" >/dev/null 2>&1; then
    return 1
  fi
  if [[ ! -f "${TMPDIR}/${asset_name}" || ! -f "${TMPDIR}/${checksums_name}" ]]; then
    return 1
  fi
  return 0
}

download_with_curl() {
  local release_tag="$1"
  local asset_name="$2"
  local checksums_name="$3"
  local token=""
  local base_url

  if token="$(github_token)"; then
    :
  else
    token=""
  fi

  base_url="https://github.com/${REPO}/releases/download/${release_tag}"
  if [[ -n "${HEYGEN_RELEASE_BASE_URL:-}" ]]; then
    base_url="${HEYGEN_RELEASE_BASE_URL%/}/${release_tag}"
  fi

  if [[ -n "$token" ]]; then
    curl -fsSL -H "Authorization: Bearer ${token}" "${base_url}/${asset_name}" -o "${TMPDIR}/${asset_name}"
    curl -fsSL -H "Authorization: Bearer ${token}" "${base_url}/${checksums_name}" -o "${TMPDIR}/${checksums_name}"
    return
  fi

  curl -fsSL "${base_url}/${asset_name}" -o "${TMPDIR}/${asset_name}"
  curl -fsSL "${base_url}/${checksums_name}" -o "${TMPDIR}/${checksums_name}"
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
  local os arch release_tag asset_name checksums_name version_output

  parse_args "$@"
  os="$(detect_os)"
  arch="$(detect_arch)"
  release_tag="$(resolve_release_tag)"
  if [[ -z "$release_tag" ]]; then
    fail "could not determine release tag"
  fi
  asset_name="heygen_${release_tag}_${os}_${arch}.tar.gz"
  checksums_name="checksums.txt"

  log "Detected: ${os} ${arch}"
  log "Installing heygen from ${REPO} release tag '${release_tag}'"

  if download_with_gh "$release_tag" "$asset_name" "$checksums_name"; then
    log "Downloaded release assets with gh"
  else
    download_with_curl "$release_tag" "$asset_name" "$checksums_name"
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
