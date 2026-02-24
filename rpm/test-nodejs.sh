#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: test-nodejs.sh <install_prefix> <expected_version>"
  echo "Example: test-nodejs.sh /export/apps/citools/OSPO-nodejs/22.15.0 22.15.0"
}

json_escape() {
  # Minimal JSON string escaper for our controlled outputs.
  local s="${1:-}"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  printf "%s" "$s"
}

install_prefix="${1:-}"
expected_version="${2:-}"
if [[ -z "${install_prefix}" || -z "${expected_version}" ]]; then
  usage
  printf '{"success":false,"error":"%s"}\n' "$(json_escape "missing required args")"
  exit 2
fi

# npm is a JS script with a `#!/usr/bin/env node` shebang; make sure our installed
# node is on PATH so `env` can find it.
export PATH="${install_prefix}/bin:${PATH}"

node_bin="${install_prefix}/bin/node"
npm_bin="${install_prefix}/bin/npm"

tests_passed=0
tests_failed=0
test_results="[]"

add_result() {
  local name="$1"
  local passed="$2"
  local details="${3:-}"
  local error="${4:-}"

  local entry
  if [[ -n "${error}" ]]; then
    entry=$(printf '{"name":"%s","passed":%s,"details":"%s","error":"%s"}' \
      "$(json_escape "$name")" \
      "$passed" \
      "$(json_escape "$details")" \
      "$(json_escape "$error")")
  else
    entry=$(printf '{"name":"%s","passed":%s,"details":"%s"}' \
      "$(json_escape "$name")" \
      "$passed" \
      "$(json_escape "$details")")
  fi

  if [[ "${test_results}" == "[]" ]]; then
    test_results="[$entry]"
  else
    test_results="${test_results%]},"$entry"]"
  fi

  if [[ "${passed}" == "true" ]]; then
    tests_passed=$((tests_passed + 1))
  else
    tests_failed=$((tests_failed + 1))
  fi
}

if [[ ! -x "${node_bin}" ]]; then
  add_result "node_exists" "false" "node binary not executable" "not found: ${node_bin}"
else
  add_result "node_exists" "true" "found ${node_bin}"
fi

if [[ ! -x "${npm_bin}" ]]; then
  add_result "npm_exists" "false" "npm binary not executable" "not found: ${npm_bin}"
else
  add_result "npm_exists" "true" "found ${npm_bin}"
fi

actual_node_version=""
if [[ -x "${node_bin}" ]]; then
  if actual_node_version="$("${node_bin}" --version 2>&1)"; then
    # node prints vX.Y.Z
    if [[ "${actual_node_version}" == "v${expected_version}" ]]; then
      add_result "node_version" "true" "actual=${actual_node_version}, expected=v${expected_version}"
    else
      add_result "node_version" "false" "actual=${actual_node_version}, expected=v${expected_version}" "version_mismatch"
    fi
  else
    add_result "node_version" "false" "" "failed to run node --version"
  fi
fi

if [[ -x "${node_bin}" ]]; then
  if out="$("${node_bin}" -e 'console.log(1+1)' 2>&1)"; then
    if [[ "${out}" == "2" ]]; then
      add_result "node_eval" "true" "node -e works"
    else
      add_result "node_eval" "false" "unexpected output: ${out}" "unexpected_output"
    fi
  else
    add_result "node_eval" "false" "" "failed to run node -e"
  fi
fi

if [[ -x "${npm_bin}" ]]; then
  if npm_ver="$("${npm_bin}" --version 2>&1)"; then
    add_result "npm_version" "true" "npm=${npm_ver}"
  else
    add_result "npm_version" "false" "" "failed to run npm --version"
  fi
fi

success="false"
if [[ "${tests_failed}" -eq 0 ]]; then
  success="true"
fi

printf '{'
printf '"install_prefix":"%s",' "$(json_escape "${install_prefix}")"
printf '"expected_version":"%s",' "$(json_escape "${expected_version}")"
printf '"node_version":"%s",' "$(json_escape "${actual_node_version}")"
printf '"passed":%d,' "${tests_passed}"
printf '"failed":%d,' "${tests_failed}"
printf '"success":%s,' "${success}"
printf '"tests":%s' "${test_results}"
printf '}\n'

if [[ "${success}" != "true" ]]; then
  exit 1
fi

