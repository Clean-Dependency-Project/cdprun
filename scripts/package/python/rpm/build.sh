#!/usr/bin/env bash
set -euo pipefail

# Python-specific packaging hook.
# This wrapper allows introducing source compilation/staging steps without
# changing Go orchestration. For now it reuses the generic RPM flow.
exec bash /workspace/scripts/package/rpm/build.sh
