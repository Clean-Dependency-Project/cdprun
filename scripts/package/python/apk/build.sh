#!/bin/sh
set -eu

# Python-specific packaging hook.
exec sh /workspace/scripts/package/apk/build.sh
