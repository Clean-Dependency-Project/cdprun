#!/usr/bin/env bash
set -euo pipefail

: "${CDP_PACKAGE_PATH:?CDP_PACKAGE_PATH is required}"
: "${CDP_RUNTIME:?CDP_RUNTIME is required}"
: "${CDP_VERSION:?CDP_VERSION is required}"
: "${CDP_INSTALL_PREFIX:?CDP_INSTALL_PREFIX is required}"

yum install -y --allowerasing ca-certificates bash rpm >/dev/null
yum clean all >/dev/null

rpm -ivh "${CDP_PACKAGE_PATH}"

if [[ "${CDP_RUNTIME}" == "nodejs" ]]; then
  bash rpm/test-nodejs.sh "${CDP_INSTALL_PREFIX}" "${CDP_VERSION}"
fi
