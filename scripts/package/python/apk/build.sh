#!/bin/sh
set -eu

# Python-specific packaging hook (follow-up phase).
# Keep this runtime-specific entrypoint so Alpine APK can adopt the same
# CDP_INPUT_PATH/CDP_INPUT_SHA256 contract used by the shared RPM flow.
exec sh /workspace/scripts/package/apk/build.sh
