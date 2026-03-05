#!/bin/sh
set -eu

# Python-specific test hook (follow-up phase).
# Keep this runtime-specific entrypoint so Alpine APK test behavior can evolve
# independently while preserving shared package execute contracts.
exec sh /workspace/scripts/package/apk/test.sh
