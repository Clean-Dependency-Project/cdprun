#!/usr/bin/env bash
set -euo pipefail

source /workspace/scripts/package/common/runtime-contract.sh
cdp_require_vars \
  CDP_PACKAGE_PATH \
  CDP_RUNTIME \
  CDP_VERSION \
  CDP_INSTALL_PREFIX

yum install -y --allowerasing ca-certificates bash rpm >/dev/null
yum clean all >/dev/null

package_path="$(cdp_workspace_path "${CDP_PACKAGE_PATH}")"

echo "installing package ${package_path}" >&2
rpm -ivh "${package_path}" 1>&2

python_bin="${CDP_INSTALL_PREFIX}/bin/python3"
if [[ ! -x "${python_bin}" ]]; then
  echo "{\"success\":false,\"error\":\"installed python binary not found at ${python_bin}\"}"
  exit 1
fi

echo "running Python functional tests" >&2
exec "${python_bin}" /workspace/rpm/test-python.py "${CDP_VERSION}"
