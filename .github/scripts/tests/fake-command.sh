#!/usr/bin/env bash
# Stands in for gh, aws and git (linked under those names). Answers only calls a
# test registered with respond or respond_fail in lib.sh, and records every call.
set -euo pipefail

tool="$(basename "$0")"
args=("$@")
jq_filter=""
dest=""

if [[ "$tool" == gh ]]; then
  # The filter is applied to the registered JSON rather than matched, so the
  # scripts' own filters are what select the output.
  rest=()
  while ((${#args[@]})); do
    if [[ "${args[0]}" == --jq ]]; then
      jq_filter="${args[1]}"
      args=("${args[@]:2}")
    else
      rest+=("${args[0]}")
      args=("${args[@]:1}")
    fi
  done
  args=("${rest[@]}")
elif [[ "$tool" == aws && "${1:-}" == s3 && "${2:-}" == cp && "${*: -1}" != - ]]; then
  # The destination is the script's temporary file, so it is not part of the key.
  dest="${*: -1}"
  args=("${@:1:$#-1}")
fi

key="$tool ${args[*]}"
printf '%s\n' "$key" >>"$FAKE_DIR/calls"
response="$FAKE_DIR/responses/$(printf '%s' "$key" | sha256sum | cut -c1-16)"
if [[ ! -d "$response" ]]; then
  printf '%s\n' "$key" >>"$FAKE_DIR/unexpected"
  echo "fake: unexpected call: $key" >&2
  exit 99
fi

cat "$response/err" >&2
if [[ -n "$jq_filter" ]]; then
  jq -r "$jq_filter" <"$response/out"
elif [[ -n "$dest" ]]; then
  cp "$response/out" "$dest"
else
  cat "$response/out"
fi
exit "$(cat "$response/rc")"
