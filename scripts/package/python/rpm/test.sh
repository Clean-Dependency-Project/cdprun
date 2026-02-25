#!/usr/bin/env bash
set -euo pipefail

# Python-specific test hook.
exec bash /workspace/scripts/package/rpm/test.sh
