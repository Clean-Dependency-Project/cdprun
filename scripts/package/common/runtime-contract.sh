#!/usr/bin/env bash
set -euo pipefail

# cdp_require_vars fails fast if any required variable is missing.
cdp_require_vars() {
  local missing=0
  local name
  for name in "$@"; do
    if [[ -z "${!name:-}" ]]; then
      echo "missing required environment variable: ${name}" >&2
      missing=1
    fi
  done
  if [[ "${missing}" -ne 0 ]]; then
    return 1
  fi
}

# cdp_workspace_path converts relative paths to /workspace-anchored paths.
cdp_workspace_path() {
  local path="${1:-}"
  if [[ -z "${path}" ]]; then
    return 1
  fi
  if [[ "${path}" = /* ]]; then
    printf "%s\n" "${path}"
    return 0
  fi
  printf "/workspace/%s\n" "${path#./}"
}

# cdp_workspace_relative removes the /workspace prefix for persisted result paths.
cdp_workspace_relative() {
  local path="${1:-}"
  if [[ -z "${path}" ]]; then
    return 1
  fi
  if [[ "${path}" = /workspace/* ]]; then
    printf "%s\n" "${path#/workspace/}"
    return 0
  fi
  printf "%s\n" "${path#./}"
}
