#!/bin/sh
set -eu

# Python-specific test hook.
exec sh /workspace/scripts/package/apk/test.sh
