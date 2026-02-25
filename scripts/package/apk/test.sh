#!/bin/sh
set -eu

: "${CDP_PACKAGE_PATH:?CDP_PACKAGE_PATH is required}"

apk add --no-cache apk-tools >/dev/null
apk add --allow-untrusted "${CDP_PACKAGE_PATH}"
